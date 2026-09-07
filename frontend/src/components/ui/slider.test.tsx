import { fireEvent } from "@testing-library/react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, describe, expect, test, vi } from "vitest";
import { Slider } from "./slider";

(
  globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }
).IS_REACT_ACT_ENVIRONMENT = true;

function mount(node: React.ReactNode) {
  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => {
    root.render(node);
  });
  return { container, root };
}

describe("Slider", () => {
  afterEach(() => {
    document.body.innerHTML = "";
  });

  test("renders the thumb's native range input with the controlled value", () => {
    const { container } = mount(
      <Slider value={4} min={1} max={16} step={0.25} aria-label="CPU" />
    );
    const input = container.querySelector(
      'input[type="range"]'
    ) as HTMLInputElement | null;
    expect(input).toBeInstanceOf(HTMLInputElement);
    expect(input?.value).toBe("4");
    expect(input?.min).toBe("1");
    expect(input?.max).toBe("16");
    expect(input?.step).toBe("0.25");
    expect(input?.getAttribute("aria-label")).toBe("CPU");
  });

  test("emits onValueChange when the hidden range input changes", () => {
    const onValueChange = vi.fn();
    const { container } = mount(
      <Slider value={2} min={1} max={16} onValueChange={onValueChange} />
    );
    const input = container.querySelector(
      'input[type="range"]'
    ) as HTMLInputElement;
    act(() => {
      fireEvent.change(input, { target: { value: "6" } });
    });
    expect(onValueChange).toHaveBeenCalledWith(6, expect.anything());
  });

  test("forwards the disabled prop to the underlying input", () => {
    const { container } = mount(<Slider value={1} disabled />);
    const input = container.querySelector(
      'input[type="range"]'
    ) as HTMLInputElement | null;
    expect(input?.disabled).toBe(true);
  });
});
