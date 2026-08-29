import { fireEvent, render } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import { ColumnResizeHandle } from "./column-resize-handle";
import { Table, TableBody, TableHead, TableHeader, TableRow } from "./table";

describe("table primitives", () => {
  test("TableBody stripes rows by default", () => {
    const element = TableBody({ children: null });

    expect(element.props.className).toContain(
      "[&_tr:nth-child(even)]:bg-control-bg/50"
    );
  });

  test("TableBody can disable striping", () => {
    const element = TableBody({ children: null, striped: false });

    expect(element.props.className).not.toContain(
      "[&_tr:nth-child(even)]:bg-control-bg/50"
    );
  });

  test("TableRow can opt out of striping", () => {
    const element = TableRow({ children: null, striped: false });

    expect(element.props["data-striped"]).toBe("false");
    expect(element.props.className).toContain("!bg-transparent");
  });

  test("ColumnResizeHandle uses a raised 12px hitbox around a 3px visual bar", () => {
    const element = ColumnResizeHandle({ onMouseDown: () => {} });

    expect(element.props.className).toContain("right-[-6px]");
    expect(element.props.className).toContain("w-3");
    expect(element.props.className).toContain("z-10");
    expect(element.props.children.props.className).toContain("w-[3px]");
  });
});

describe("TableHead sorting", () => {
  function renderSortTable(
    headProps: Partial<Parameters<typeof TableHead>[0]> = {}
  ) {
    const onSort = vi.fn();
    const { container } = render(
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead sortable onSort={onSort} {...headProps}>
              Name
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow>
            <td>cell</td>
          </TableRow>
        </TableBody>
      </Table>
    );
    const head = container.querySelector("th") as HTMLElement;
    const handle = container.querySelector(".cursor-col-resize") as HTMLElement;
    return { onSort, head, handle };
  }

  test("sortable header toggles sort on click", () => {
    const { onSort, head } = renderSortTable();
    fireEvent.click(head);
    expect(onSort).toHaveBeenCalledTimes(1);
  });

  test("onClick preventDefault blocks sorting", () => {
    const { onSort, head } = renderSortTable({
      onClick: (e) => e.preventDefault(),
    });
    fireEvent.click(head);
    expect(onSort).not.toHaveBeenCalled();
  });

  test("resize drag on the handle does not trigger sort", () => {
    const onResizeStart = vi.fn();
    const { onSort, head, handle } = renderSortTable({
      resizable: true,
      onResizeStart,
    });

    fireEvent.mouseDown(handle);
    expect(onResizeStart).toHaveBeenCalledTimes(1);
    // A drag release over the handle synthesizes a click that must not sort.
    fireEvent.click(handle);
    expect(onSort).not.toHaveBeenCalled();
    // A release over the header itself also synthesizes a th click that must
    // not sort once the drag re-arms on the next genuine press.
    fireEvent.click(head);
    expect(onSort).not.toHaveBeenCalled();
    // The next genuine press + click sorts again.
    fireEvent.mouseDown(head);
    fireEvent.click(head);
    expect(onSort).toHaveBeenCalledTimes(1);
  });
});
