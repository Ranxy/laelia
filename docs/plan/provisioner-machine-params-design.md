# Provisioner — User-Configurable Machine Parameters — Design

> Status: **implemented** (verified against the code on 2026-09-07). This
> document is the reference for future changes to machine-parameter handling.
> Extends `docs/plan/provisioner-design.md` (§14 "Per-machine spec overrides").
> Deploy-time config: `docs/deploy.md` §8.3 + `charts/provisioner`.

## 1. Decision record (confirmed with product owner)

| # | Question | Decision |
|---|----------|----------|
| 1 | Who defines the parameter universe? | **Hybrid**: the manager owns a fixed parameter *catalog* (keys + value semantics); each provisioner reports at connect time which catalog entries it exposes, plus per-instance constraints. |
| 2 | Admin governance | **Trust the provisioner's declared bounds** (min/max from its config). No workspace switch/allowlist — "who may provision" is already IAM-governed. |
| 3 | Defaults | **The provisioner config stays the source of defaults**, reported with the schema as the form placeholder. No manager-side defaults. |
| 4 | Change after creation? | **No** — create-time only. Changing sizing = delete + re-create (see §6 for the disk caveat). |
| 5 | First parameter set | **`cpu`, `memory`, `disk`** (+ `storage_class` when configured). The mechanism is backend-agnostic. |

## 2. Model

```
Manager catalog (code-fixed)     cpu/memory/disk = QUANTITY, storage_class = STRING
  ↑ provisioner reports a subset  ProvisionerReady.machine_params (defaults + bounds from its config)
  ↓ persisted                     provisioner.status.machine_params (jsonb; form renders while offline)
  ↓ user values, validated        machine.provisioning.machine_params (jsonb; per-machine overrides)
  ↓ shipped verbatim              ProvisionMachineJob.machine_params → MachineSpec.Params → CR merge
```

- **Catalog** (`backend/manager/component/provision/machine_params.go` +
  key constants in `backend/common/machineparam`): keys → value type. The
  type is manager knowledge, never provisioner-reported; reported keys outside
  the catalog are dropped (logged), so a newer provisioner + older manager
  degrades gracefully. Keys are wire strings — adding one never changes proto
  fields.
- **Schema** (`MachineParamSpec`): per-instance `required` / `default_value`
  (config) / `min_value`–`max_value` / `options`, persisted in
  `provisioner.status` at every `ProvisionerReady`.
- **Values**: `map<string, string>` on `ProvisionMachineRequest`, validated
  manager-side, persisted per machine, shipped **verbatim**. Absent/empty keys
  keep falling back to config defaults (untouched fields track admin config).
- Backward compatibility is total in both directions: old provisioner + new
  manager (no schema → no form fields, request params rejected) and new
  provisioner + old manager (unknown fields ignored → config defaults, exactly
  the pre-feature behavior).

## 3. Proto surface (field numbers are contract — never reuse)

| Message | Field | Where |
|---|---|---|
| `MachineParamType` (QUANTITY=1, STRING=2) | — | store + v1 mirrored |
| `MachineParamSpec` | key=1 required=2 default_value=3 min_value=4 max_value=5 options=6 | store |
| `MachineParamSpec` (adds type=2, rest shifted) | key=1 type=2 required=3 default_value=4 min_value=5 max_value=6 options=7 | v1 |
| `ProvisionerStatus.machine_params` (persisted schema) | 8 | store + v1 |
| `ProvisioningStatus.machine_params` (per-machine values) | 9 | store + v1 (`machine.proto`) |
| `ProvisionerReady.machine_params` (report, no type) | 7 | v1 |
| `ProvisionMachineRequest.machine_params` (user input) | 5 | v1 |
| `ProvisionMachineJob.machine_params` (job payload) | 11 | v1 |

Regenerate `backend/generated-go/` + `frontend/src/types/proto-es/` together.
Both persistence points are existing jsonb columns — **no SQL migration**.

## 4. Manager behavior

`backend/manager/component/provision/machine_params.go`:

- **`ValidateMachineSchema`** (stream, every Ready frame → `handleReady`):
  drops unknown keys (logged), duplicates (first wins), entries whose default
  or bounds fail validation, and clears bounds on STRING params; a malformed
  report can never poison the persisted schema.
- **`ValidateMachineParams`** (API boundary; every failure is
  `InvalidArgument`): key ∈ catalog → key declared by the target provisioner's
  persisted schema → type shape (QUANTITY via `backend/common/quantity`;
  STRING via DNS-1123 label) → inclusive min/max bounds → options membership.
  Values are trimmed, then persisted **verbatim** (no normalization like the
  image ref); empty values are treated as absent. Values ≤ 64 chars.
- **`composeProvisionMachineJob`**: copies
  `machine.provisioning.machine_params` verbatim. **Never re-validate on the
  replay path** — replay rebuilds the workload the user asked for even if the
  schema shrank in between (same precedent as `runtime_image`).
- `convertToV1MachineParams` fills `type` from the catalog for the UI.
- `backend/common/quantity`: self-contained k8s-quantity parser/comparator
  (subset: `m k M G T P Ki Mi Gi Ti Pi`; no sign, no exponent, ≤ 3 fractional
  digits, milli-unit resolution, E-scale rejected as overflow). The manager
  stays k8s-library-free; the k8s API server remains the authoritative
  validator.
- Wiring lives in `api/v1/provisioner_stream.go` (`machineParamsFromReady`,
  `cloneStoreProvisionerStatus` — note: this clone also preserves
  `RetainData`, which it used to drop), `api/v1/provisioner.go`
  (`ProvisionMachine`, `composeProvisionMachineJob`,
  `convertToV1MachineParams`, `cloneProvisioningStatus`) and
  `api/v1/machine_convert.go`.

## 5. Provisioner behavior

- **Config** (`cmd/config.go`): `param_bounds` (catalog key → `{min, max}`,
  k8s quantities, omitted side unbounded) — folded into `config_digest`.
  Defaults stay the existing `resources` / `storage` keys. Also exposed as
  Helm values (`charts/provisioner`: `paramBounds`).
- **Schema report**: `Backend.MachineParams()` (interface method) builds the
  Ready-frame schema; the client converts it store-shaped (`machineParamsReady`).
  The k8s backend declares `cpu`/`memory`/`disk` always (disk's default is the
  *resolved* PVC size, not the raw config), `storage_class` **only when**
  `storage.storage_class` is set (never offer free-text class names against an
  unknown cluster).
- **Apply** (`kubernetes/backend.go` `applyMachineParams`):

| Param | CR mapping |
|---|---|
| `cpu` | `requests.cpu = limits.cpu = value` |
| `memory` | `requests.memory = limits.memory = value` |
| `disk` | `spec.storage.size = value` |
| `storage_class` | `spec.storage.storageClass = value` |

  CPU/memory set request **and** limit to the same value — one knob,
  Guaranteed QoS (asymmetric limits would need separate `cpu_limit`-style
  catalog keys; no proto change required). Absent keys fall back to
  `cfg.Resources`/`cfg.Storage` exactly as before. **Backends apply only keys
  they know and ignore the rest** — merge, never reject: a key persisted by a
  newer manager (or replayed after a catalog change) must never fail an older
  provisioner binary. The mock backend records `Params` (`ParamsFor`) for
  tests; the docker stub is unaffected.

## 6. Replay & edge cases

| Scenario | Behavior |
|---|---|
| Replay after schema shrank / bounds tightened | Persisted params ship verbatim; no re-validation. |
| Value passes the manager but the backend/API server rejects it | Job → `FAILED` with the backend error on the machine row. Manager validation keeps this rare. |
| Old provisioner (no schema) | No form fields; request params → `InvalidArgument`. |
| New provisioner, old manager | Unknown Ready/job fields dropped; config defaults apply. |
| **Disk param changes for an existing workload** | The reconciler deliberately preserves `volumeClaimTemplates` (immutable in k8s), so the PVC keeps its created size and the CR's `storage.size` may drift cosmetically. Persisted params matter on the *rebuild* path (replay-before-creation, recreate after manual teardown) — the rebuilt StatefulSet carries the persisted param, not the current config default. |
| cpu/memory on replay | Mutable StatefulSet fields; applied on the next reconcile. |
| Provisioner offline | `ProvisionMachine` fails fast; the form still renders from the persisted schema. |

## 7. Frontend

- **Create form** (`machine-new-provisioned.tsx`): one input per schema entry
  for the selected provisioner; inputs start **empty**, `default_value` is the
  placeholder, min/max render as a range hint; only non-empty trimmed values
  submit (`stores/provisioner.ts` `provisionMachine` 4th arg). A key unknown
  to the frontend renders under its raw catalog name.
- **Profile card** (`machine-profile-cards.tsx`): persisted
  `provisioning.machineParams` shown read-only, key-sorted.
- Labels come from `src/lib/machine-params.ts` → `machine.param.<key>` locale
  keys (`machine.param.` is registered in `check-react-i18n.mjs`
  `DYNAMIC_PREFIXES` because usage is via `t(labelKey)` indirection).

## 8. Security & IAM

Only catalog keys reach a pod spec, and every value is tightly shaped — no
free text beyond what the k8s API server re-validates. Bounds come from the
admin-owned provisioner config (same trust as `resources`/`storage`). IAM is
unchanged: parameters ride `ProvisionMachine`, so
`laelia.provisioners.provision` remains the only right needed (custom-image
precedent).

## 9. How to add a catalog parameter (e.g. `gpu`)

1. `backend/common/machineparam`: add the key constant.
2. `backend/manager/component/provision/machine_params.go`: add it to
   `catalog` with its type (value validation is then automatic).
3. Give it semantics per backend: declare it in
   `kubernetes/backend.go` `MachineParams()` and apply it in the
   `applyMachineParams` switch (unknown keys stay ignored).
4. Frontend: label entry in `src/lib/machine-params.ts` + `machine.param.<key>`
   in `en-US.json`/`zh-CN.json` (run `pnpm --dir frontend sort:i18n`).
5. No proto change (string-keyed map is the stable wire form); add tests for
   the new type/mapping.

Non-goals (no proto churn needed when revisited): separate
`cpu_limit`/`memory_limit` params, enum `storage_class` with cluster-enumerated
options (needs cluster-scope RBAC), post-creation resize, per-workspace size
caps, docker/VM backend parameter sets.

## 10. Design rationale — do not undo these

- **Per-machine persistence + verbatim replay are load-bearing**: without
  them, a PENDING replay (provisioner reconnect mid-provision — the common
  case) silently rebuilds the CR from config defaults, overriding the user's
  request. `runtime_image` is persisted for the same reason.
- **The manager catalog** is the cheap anchor for fail-fast validation, i18n
  labels, and uniform naming across backends; dropping it means trusting the
  provisioner's report for semantics.
- **Manager-side validation is the fail-fast layer, not the authority** — the
  k8s API server re-validates everything; the manager check exists so users
  learn at click time, not minutes later as a FAILED machine.
- **Self-contained quantity parser** (`backend/common/quantity`) keeps the
  manager k8s-library-free; `k8s.io/apimachinery` is the documented
  alternative if the subset ever becomes a maintenance burden.
- **Bounds are the only size guard**: shape validation alone would let
  `cpu="1m"` / `disk="999Ti"` through to scheduling/PVC failures.
- Tests pin all of this: `backend/common/quantity`, `component/provision`
  (`machine_params_test.go`), `api/v1` (Ready/persist/convert),
  k8s backend merge tests, client passthrough, and the create-form suite.