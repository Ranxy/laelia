# Agent 命令执行与上报解耦(outbox)架构设计

> 状态:2026-10-10(第二版)。第一版分析并落地了 resume-on-BeginSession(`611aa69` 僵尸清理、`7b0fbfe` resume),经评审确认那只是**过渡修复**——它把"流断→turn 死"的事故变成了"中断→续跑"的低效循环,但 turn 与流的耦合仍在,由此派生的一系列问题(livelock、副作用重复、seq 冲突)无法根治。本版按评审确定的方向重写:**turn 执行与数据上报彻底解耦**,machine 本地持久化缓冲 + 幂等重传(outbox 模式),上报通道改为无长连接的 unary 批量 RPC。原文档中的候选方案 B~F 均被本设计取代。

---

## 一、问题回顾

### 1.1 根因(不变)

部署形态 `machine → Caddy(HTTPS/h2) → Traefik ingress(h2c) → manager pod(h2c)`。Traefik v2.11.2+/v3 entrypoint 默认 `respondingTimeouts.readTimeout = 60s`,**从请求开始绝对计时**(非空闲超时,15s ping 救不了)。machine 的 MachineChannel / AgentChannel 是永不结束的双向流,每条流打开后恰好 60s 被杀。本地部署(Caddy → localhost)无此超时,故不复现。

### 1.2 为什么 resume-on-BeginSession 不是终点

当前架构里,**turn 的执行循环和上报通道是同一个生命周期**:drain loop 的 ctx 派生自 stream ctx;`runCommand` 的每条 progress/event/result 直接对 bidi 流发送,任一条发送失败即中止 turn(`drain_runner.go` 四处 `return nil`);机器断线时 `teardownRunners` 连 in-flight turn 一起杀掉(`runner.go`)。turn 的生死由一条长连接的健康度决定——这正是 60s 杀流事故的根源。resume 机制只是让循环"断了再续",由此留下旧文档 P1~P11 的一串问题,其中最重的两个:

- **P3 livelock**:单步骤 >55s 且无中间产出时,每个 60s 周期都死于步骤完成前,resume 后重试同一死步骤,永远活锁;
- **P1 副作用重复**:每次 resume 是"重新发起一轮 LLM turn",中断点落在非幂等操作上时只能靠 LLM 自查。

根因是耦合本身,补丁无法消除耦合。根治方向:**把 turn 从网络的正确性包络里拿出来**。

---

## 二、目标架构:执行面 / 上报面 / 控制面三面拆分

### 2.1 设计原则

1. **执行面自治**:agent turn 的执行不依赖 manager 的任何同步应答。turn 循环对网络的唯一动作是"向本地 outbox 追加记录"(进程内 + 本地磁盘,不涉及网络)。
2. **上报面异步幂等**:machine→manager 的全部数据流(progress / event / result)经独立的 uploader 走 unary 批量 RPC;传输为 at-least-once,manager 以 `(command_id, seq_no)` 幂等去重,合成为"每条事件最终恰好入库一次"。
3. **控制面单一**:manager→machine 的全部反向交互(取消、steer、wake、配置、prompt 通知)收敛到 MachineChannel 一条控制流;其死亡**只影响控制交互与在线状态,不影响任何正在执行的 turn**。
4. **manager 数据库仍是用户可见状态的唯一事实源,但它是上报数据的投影**:命令状态由上报的终态事件驱动,不再由连接状态驱动。

### 2.2 三面与数据流

```
┌─ machine ─────────────────────────────────────────────┐
│  runner(每 agent,长生命周期,不随任何流生灭)          │
│   └─ drain loop ── turn 执行(LLM 子进程 + 本地工具)   │
│        └─ 产物(-progress/event/result)──> outbox     │
│             (每 agent 一个,tidwall/wal 分段日志)      │
│   ┌─ uploader(独立 goroutine,退避重连)               │
│   │    outbox ──批量──> UploadCommandData ──ack──> 驱逐 │
│   └─ control:MachineChannel(assignment/取消/steer/wake)│
└───────────────────────────────────────────────────────┘
            │ unary(批量上传,无长连接)   │ bidi(仅控制)
            ▼                             ▼
┌─ manager ─────────────────────────────────────────────┐
│  UploadCommandData:单事务按序落库(幂等),终态驱动      │
│  命令状态机;响应携带 per-command last_ack_seq          │
│  BeginSession(unary):工作发现,mint command            │
│  MachineChannel:控制推送 + 断线积压控制消息重放         │
│  reaper:仅"机器失联超 grace"时收尾 RUNNING             │
└───────────────────────────────────────────────────────┘
```

关键解耦点:**agent(LLM 子进程)→ machine daemon 本地链路本来就是可靠的**(stdio/本地 socket,现状不变);重构只发生在 machine→manager 一跳,把"同步转播"改为"持久缓冲 + 异步补传"。

### 2.3 失败模型(新旧行为对照)

| 场景 | 旧(resume 方案) | 新(outbox 方案) |
| --- | --- | --- |
| 流被代理 60s 杀死 | turn 中断 → resume 续跑,每周期损一步 | **turn 不受任何影响,照常跑完**;上报短暂延迟 |
| 流反复被杀(P3 livelock 场景) | 长步骤永远活锁 | turn 一次跑完,livelock 类别消失 |
| 断线窗口内事件 | 永久丢失,WARNING 标记空洞 | 全部保留,重连后按序补传 |
| result 发送"成功"但实际未达 | state 被清 → seq 冲突 → 早期事件被吞(P2) | 终态仍在 outbox,未 ack 即重传,幂等去重 |
| 用户断线期间取消 | `CancelCommand` 报 "agent not connected" | manager 排队,**重连后送达**(决策 ①) |
| 机器彻底失联 > grace | grace → FAILED | grace → FAILED(reason=失联);机器回归后按决策 ② 对账 |
| 机器进程崩溃重启 | state 文件续跑(仅 pi/acp 会话尚存时) | turn 确认丢失;启动时自检 outbox,补报 FAILED 终态(§3.6) |
| manager 整体不可达 | turn 中断失败 | turn 继续;数据面工具(读写会话等)照旧失败——固有边界(§3.8) |

---

## 三、组件与协议设计

### 3.1 machine 端 durable outbox(存储实现:tidwall/wal)

不自研日志格式与轮转/截断逻辑,采用 `github.com/tidwall/wal`(MIT;依赖仅 `tidwall/gjson` + `tidwall/tinylru`,版本 pin)作为 outbox 存储:

- **结构**:每 agent 一个 WAL 实例,目录 `<data>/<machineID>/<agentID>/outbox/`(该目录即 WAL 本体,分段文件按默认 `SegmentSize=20MB` 自动轮转,无需自写轮转)。每条记录是一帧信封:`{command_id, kind: progress|event|result, seq, timestamp, payload}`,payload 字段与现有 `CommandProgress` / `CommandEvent` / `CommandResult` 一致。
- **两套序号,职责分离**:WAL 的 index(uint64,严格 `LastIndex()+1` 单调无间隙,`ErrOutOfOrder` 强制)只是**日志内部序位**,驱逐游标用,不外泄;manager 的幂等键仍是信封内的 per-kind `seq`——progress / event 是两个既有 seq 空间,`ON CONFLICT (command_id, seq_no)` 与 Watch API 语义不动。resume 退役后每 turn 独立从 1 计数,`executor.LocalState` 的 seq 续接与 resume 检测职责整体消失。
- **写路径**:turn loop 追加进内存缓冲,按(时间 100~200ms 或字节 64KB)阈值 `WriteBatch` 落盘;库默认 `NoSync=false` 即**每批 fsync**——掉电不丢已写记录(量级:每秒数批,fsync 开销可忽略;性能需要时可换 `NoSync` + 周期 `Sync()`,见 §七)。落盘失败属机器本地故障,turn 报失败(与现有磁盘故障语义一致)。
- **读路径**:uploader 按区间 `FirstIndex..LastIndex` 逐条 `Read` 信封、组批上传。库内部单把 `RWMutex` 已保证并发安全,turn 追加与 uploader 读取可分 goroutine 共享同一实例;仍维持单写者纪律。
- **驱逐(ack 驱动)**:上传响应给出 per-(command,kind) 水位(§3.2);uploader 计算覆盖边界 `F`(最后一个"其前所有记录均已覆盖"的 index),`TruncateFront(F+1)` 丢弃已持久化前缀;`result_acked` ⇒ 该命令整段已持久化 ⇒ `TruncateFront(LastIndex+1)` 清空。**必须 `Options.AllowEmpty=true`**:否则全量驱逐后的日志再次 `Open` 会报 `ErrEmptyLog`,且截断"最后一条记录"会被 `ErrOutOfRange` 拒绝。
- **损坏处理**:二进制帧为长度前缀(uvarint size + data),**无校验和**;撕裂写可在载入时检测(`ErrCorrupt`)而非静默错读。因默认每批 fsync,撕裂只可能发生在掉电/内核崩溃(进程崩溃不会撕裂已写批次)。策略:`Open`/操作遇 `ErrCorrupt` → 先做一次 `Close`+`Open` 重试;仍损坏 → 将该 WAL 目录**改名隔离**(quarantine)+ 显式告警,按空 outbox 继续——损失仅限该 agent 未上传尾部,由 reaper/迟到改判规则兜底(§3.6)。
- **关键性质不变:每 agent 的 outbox 同时至多含一条命令的待传数据**(turn-start barrier,§3.4);机器崩溃后残留记录由 §3.7 自检收敛。

### 3.2 上报协议:unary 批量上传 + ack

- 新 RPC(挂在现 AgentStreamService 名下,保留 machine token 的 CUSTOM 鉴权,或并入 MachineStreamService 域,实现时定):

  ```
  rpc UploadCommandData(UploadCommandDataRequest) returns (UploadCommandDataResponse);
  // Request:  entries[] { command_id, seq_no, kind, payload, agent_side_timestamp }
  //           (混合多命令合法;per-command 内严格按 seq 升序)
  // Response: acks[] { command_id, last_progress_seq, last_event_seq, result_acked }
  //           // per-(command,kind) 持久化水位;result_acked ⇒ 该命令本批及之前记录全部已持久化
  ```

- **幂等**:manager 落库沿用现有 `command_output` / `command_event` 的 `ON CONFLICT (command_id, seq_no) DO NOTHING`(`store/command.go:429,441`);重传只增不乱。
- **ack 与驱逐**:响应携带 per-command 水位 `{last_progress_seq, last_event_seq, result_acked}`(manager 幂等落库后回传本批各 kind 的最大 seq;事件侧与现有 `command.last_ack_seq` 维护逻辑一致,`command_handler.go:144,183`)。uploader 以单调最大值推进内存水位,再按 §3.1 计算覆盖边界并 `TruncateFront`。机器重启后水位无需持久化:直接从日志区间重新上传,幂等去重兜底。
- **批次事务**:manager 在**单个事务**内按序应用整个 batch;终态 result 作为批内最后一条处理,保证"命令状态转换"与"事件历史"在同一事务内一致。
- **归属校验**:每个 entry 校验 `command.agent.machine_id == 认证 machine`,等价于现在 AgentChannel 的 `resolveAgentForMachine`。
- **poison message**:manager 校验失败(未知事件类型、归属不符)必须**显式拒绝并携带被拒 seq**;uploader 收到后丢弃该条(本地留日志),绝不能无限重试卡死队列。终态被拒的命令由 manager reaper 兜底。
- **上报延迟**:批窗口(默认 ~200ms)决定 UI 实时性;对比现在流式推送,延迟感知差异可忽略。
- **退避**:uploader 独立 goroutine,指数退避重连重传;重连风暴只影响上报延迟,与 turn 完全无关(P11 就地消解)。

### 3.3 turn loop 改造点(全部是删除耦合,非新增机制)

1. `runCommand` 四处"发送失败 → `return nil` 中止 turn"(`drain_runner.go:495,511,575,559`)→ 全部改为"追加 outbox";网络发送从 turn 路径中**完全消失**。
2. `runSession` / drain loop 的 ctx 不再派生自 stream ctx(`runner.go:264-267`),改为派生自 runner 的长生命周期 ctx。
3. `teardownRunners`(断线杀掉全部 runner 含 in-flight turn,`runner.go:625-638`)→ 断线只关流与 uploader,runner(执行态)保留;runner 生命周期改由 assignment(增/删)与机器退出驱动。
4. result 语义变更:turn 结束 = 终态记录**落盘成功**,而非"发送成功"——`resultSent` / 先发结果再清 state / `ClearLocalState` 等补偿逻辑全部删除。
5. `serializedSender` / `beginRespCh` / 陈旧回包丢弃 / `beginSessionResponseTimeout`——随流退役一并删除。

### 3.4 命令边界串行化(turn-start barrier)

**规则:开始命令 N+1 的 turn 之前,该 agent outbox 中命令 N 的全部记录(含终态)必须已上传并 ack。**

- 这不是可选项,是正确性要求:否则重连后 manager 侧会出现同一 agent 双 RUNNING。
- **零代价**:新命令只能由 manager 投递,manager 可达时 flush 必然成功;manager 不可达期间 drain loop 本来就拿不到新命令。
- 死锁防护:若命令 N 的终态被 manager 拒收(poison),uploader 按 §3.2 丢弃后 barrier 放行,并本地记录。

### 3.5 控制面收敛(决策 ①:AgentChannel 退役,unary 上报)

- **`AgentStreamService.AgentChannel` 双向流退役**。现流上的消息去向:
  - `Progress` / `Event` / `Result` → UploadCommandData(§3.2);
  - `BeginSession` → **改 unary RPC**(本就是"agent 拉工作"语义);响应(命令 id、display names、team、prompt_version、release notice)不变;`beginRespCh` 那一类跨连接状态机消失;
  - `Ping/Pong` → 随流退役(MachineChannel 已有 ping/pong 承担机器存活);
  - `AgentReady` → 退役;agent 身份绑定改由 upload/BeginSession 的归属校验承担;
  - `ProvidersDiscovered` / `WorkspaceList/ReadResponse`、`DiscoverProviders` / `WorkspaceList/ReadRequest` → 并入 MachineChannel(该流已有同类请求-响应对:assignment / upgrade / workspace scan / models 发现,`machine.proto:571-585`,机械扩展);
  - `CancelMessage` / `SteerMessage` / `NewMessagesAvailable`(wake)/ `PromptReleaseNotice` → ManagerMachineStreamMessage 新增对应消息,machine 端路由到对应 runner 的现有入口(message_router 的 cancel/steer 分支逻辑复用)。
- **断线期间的控制消息(决策 ①:仅重连后生效)**:manager 新增 `agent_pending_control` 表;`CancelCommand` / `SteerCommand` 遇机器不在线时入队(代替现在的直接报错),API 返回"已排队,机器恢复连接后送达";MachineChannel(重)连时按序派发。派发前 manager 校验:命令已有终态则丢弃(cancel 不追杀已结束命令;steer 无意义)。**明确代价**:断线期间取消不生效,命令可能继续运行至完成并烧 token;迟到结果按 §3.6 对账规则处理(用户取消的命令永不复活)。

### 3.6 状态对账(决策 ②:允许迟到结果改判)

manager 侧命令状态机规则:

1. **终态只由上报驱动**:COMPLETED/FAILED 由 UploadCommandData 中的终态记录驱动(批内最后一条),与事件同事务。
2. **改判规则**:`FAILED(reason=machine-unreachable)` 的命令,若其后收到成功终态,允许改判 COMPLETED,并补录一条 SYSTEM 事件说明改判原因(命令在失联期间被超时标记失败,现按迟到结果改判)。为此 command 表需区分失败原因(建议新增 `failure_kind` 列:`user_cancel` | `machine_unreachable` | `agent_failed`;迁移走 LATEST.sql + 增量文件双轨)。
3. **用户取消不可改判**:CANCELED(或 `failure_kind=user_cancel` 的 FAILED)收到迟到终态时状态不变,迟到产物仅作为补录事件入库(带 late 标记),供审计。
4. **reaper 新语义**:仅当机器**失联**(MachineChannel 不在线)超过 grace 时,才把该机器名下 RUNNING 命令标 FAILED(`failure_kind=machine_unreachable`)。"10 分钟无事件"不再作为 reap 依据——在线机器的 turn 静默(长 LLM 思考、长命令)是合法状态;原 `currentCmdID` 豁免逻辑随之消亡。
5. grace 期间 UI 可显示"机器离线,结果待补传"(可选,不阻塞)。

### 3.7 机器重启自愈闭环

turn 不再被流检测,机器彻底崩溃时命令无人收尾。闭环:

- 机器启动时重开各 agent 的 WAL 并扫描全部信封:存在记录但**无终态记录**的命令 → 追加合成的 FAILED 终态("machine restarted mid-turn",`failure_kind` 标注)→ 随正常上传补报;
- 有终态但未 ack 的记录 → 照常重传(幂等);
- manager 侧无需新增机制:失联超 grace 的 reaper + 迟到改判规则覆盖其余路径。

### 3.8 留存与上限(决策 ④)+ 明确不做的事

- **留存**:未 ack 记录保留直到 ack;机器级总量上限(默认 512MB,可配置)作为兜底——触顶时按最旧命令**整组**丢弃(WAL 上即按命令组边界 `TruncateFront`,丢弃只写本机日志),被丢命令若 manager 侧仍 RUNNING 由 reaper 收尾。正常量级为每 turn 数 MB,上限几乎不会触达。
- **明确不做的事(能力边界,写死避免误解)**:
  - 解耦消除的是"**流死亡**"耦合,不是"manager 整体不可达"耦合——断线期间依赖 manager 数据的工具(读写会话、发消息、任务/提醒)仍会失败,这是固有的;
  - 机器进程崩溃仍丢失进行中的 turn(检测 + 补报终态,不复活;不做 turn 级进程快照恢复);
  - 断线期间进度静止,重连后一次性补传(可选项:presence 缺失时 UI 显示"上报延迟")。

---

## 四、旧问题消解矩阵(P1~P11 → 新架构)

| 旧问题 | 状态 | 依据 |
| --- | --- | --- |
| P1 副作用重复/悬空 tool call | **消解** | 流死不再打断 turn;唯一剩余路径是机器崩溃,而该路径不重试 turn |
| P2 result 在途丢失 → seq 冲突 | **消解** | 终态落 outbox,"已发送"语义变为"已 ack";seq 由日志承载,无重排 |
| P3 长步骤 livelock | **消解** | turn 与流解耦,步骤任意长都能跑完 |
| P4 隐式 resume 协议 | **消解** | resume 机制整体退役;ack 显式出现在上传响应中 |
| P5 断点窗口事件丢失 | **消解** | outbox 持久化缓冲 |
| P6 每次中断重定向成本 | **消解** | 无 resume,turn 不再被打断 |
| P7 无限 churn 无熔断 | **消解** | 无 churn;缓冲增长由上限兜底(§3.8) |
| P8 断线期间用户意图丢失 | **按决策 ① 收敛** | 取消排队、重连送达;断线中取消延迟生效是接受的代价 |
| P9 慢重连仍失败 | **消解** | turn 不依赖重连;grace 只影响对账不再影响执行 |
| P10 `AgentReady.lastCommandId` 语义漂移 | **消解** | 消息退役;`command.last_ack_seq` 保持为 manager 侧持久化游标,上传响应回传 per-kind 水位驱动驱逐 |
| P11 重连风暴放大 | **消解** | 流只剩 MachineChannel 一条;上报退避独立,不冲击 turn |

新引入的代价(均在 §3/§7 落设计):磁盘持久化路径成为执行面新增依赖(N1)、WAL 库引入的依赖与损坏降级策略(N2)、上报延迟一个批窗口、控制面消息搬家、machine/manager 协议同步升级窗口(§六,受嵌入升级机制保护)。

---

## 五、已定决策记录(2026-10-10 评审)

1. **断线期间 cancel/steer:仅重连后生效**(不引入轮询)。理由:实现最简单;接受断线期间命令可能白跑的代价,由 §3.5 排队派发 + §3.6 对账规则兜住正确性。
2. **迟到结果:允许改判**。`machine_unreachable` 失败可被迟到成功终态改判 COMPLETED(补录说明事件);用户取消永不改判,迟到产物仅补录。
3. **上报通道:unary 批量上传,退役 AgentChannel**。彻底消灭"长连接被代理杀"这一类故障在上报面的存在;反向控制交互并入 MachineChannel。
4. **outbox 留存:保留直到 ack + 上限兜底**(512MB/机器,可配)。
5. **outbox 存储实现:tidwall/wal(2026-10-10 补充)**:不自写磁盘日志格式;分段轮转、单调索引、前缀截断、撕裂写检测交给库(默认每批 fsync)。代价:新增两个轻量依赖(gjson/tinylru);`ErrCorrupt` 时需隔离目录降级(§3.1);必须 `AllowEmpty=true` 规避空日志 `ErrEmptyLog`。

---

## 六、迁移路径(允许大重构;分四阶段,每阶段独立可验证)

> 版本同步说明:machine 二进制由 manager 嵌入下发(自升级走 UpgradeRequest),双端协议演进天然同步,不存在长期混布窗口;以下顺序只为控制单次变更的回归面。

1. **阶段 1 — outbox + 幂等上传(上报面切换)**:machine 端新增 outbox(实现采用 `github.com/tidwall/wal`,`go mod tidy` 引入)+ uploader;manager 新增 UploadCommandData(单事务 + ack + 归属校验 + poison 拒收);progress/event/result 停止走流,AgentChannel 暂保留(旧消息类型不再使用)。ack 水位驱动 `TruncateFront` 驱逐。
2. **阶段 2 — 执行面与流解绑**:runner/turn 生命周期上移(§3.3 四项);teardown 语义改为"断线只关流"。此阶段结束后,60s 杀流对命令已完全无感。
3. **阶段 3 — 控制面收敛与对账**:cancel/steer/wake/prompt-notice/workspace 并入 MachineChannel;BeginSession 改 unary;断线积压控制消息(pending 表 + 重连派发);退役 AgentChannel 与全部 resume 机制(`pickResumeCommand`、resume notice/WARNING、load-or-init 的 resume 分支、stale-reply 处理);reaper 新语义 + `failure_kind` 迁移 + 改判规则。
4. **阶段 4 — 自愈与兜底**:机器启动自检补终态(§3.7);outbox 上限与告警(§3.8);清理 `AgentReady`/`lastCommandId`/`resumeTurnNotice` 等退役协议痕迹。

---

## 七、剩余风险与开放问题

1. **N1 磁盘路径成为执行面新增依赖**:outbox 所在盘故障/写失败 = 未传数据丢失,命令由 reaper 收尾——与现状(数据仅存内存)相比严格变好。落盘失败时 turn 中止并报错(与磁盘故障语义一致,§3.1)。WAL 默认每批 fsync,掉电仅可能损失正在写的最后一批;撕裂写由库检测(`ErrCorrupt`),按隔离目录降级(§3.1)。
2. **N2 WAL 参数与版本**:库默认参数(`SegmentSize=20MB`、`AllowEmpty=true`、`NoSync=false`)需在压测中确认;依赖版本 pin 后锁定;若上游久不维护(最后更新较早),需评估 fork 或抽象一层薄接口以便替换。
3. **N3 投影一致性窗口**:manager 长时间不可达时,RUNNING 命令长时间无事件。presence 缺失时的 UI 提示(§3.8 可选项)是否要做,影响前端工作量。
4. **N4 协议兼容窗口**:阶段 1~3 期间新旧机制并存,回归测试矩阵(新 machine/旧 manager 不支持——但受嵌入升级机制保护,窗口实际很短)。
5. **N5 批参数与上报时延**:批量大小 / flush 间隔 / 退避曲线需要压测定参;上传失败的重试风暴在多 agent 机器上的 manager 写入压力(每命令批事务)待观测。
6. **与 Traefik 修复的关系**:部署指引(`docs/deploy.md`:禁用 entrypoint readTimeout)仍是**运维上的根治**,本方案是**架构上的根治**——两者独立成立;本方案落地后,任何代理超时形态(60s 绝对、nginx `proxy_read_timeout`、任意中间盒)对命令执行均无影响,部署指引降级为性能/资源层面的建议。