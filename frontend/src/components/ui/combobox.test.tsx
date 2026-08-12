import { act, useState } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { ModelCombobox } from "./combobox";

(
  globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }
).IS_REACT_ACT_ENVIRONMENT = true;

// Set a controlled React input's value and fire onChange the way a real
// keystroke would. Setting `.value` directly does not trigger React's onChange
// in jsdom; the prototype setter + `input` event does.
function typeInto(input: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(
    HTMLInputElement.prototype,
    "value"
  )?.set;
  setter?.call(input, value);
  input.dispatchEvent(new Event("input", { bubbles: true }));
}

// A tiny harness that records the chosen value so tests can assert on it.
function Harness({ initial = "" }: { initial?: string }) {
  const [value, setValue] = useState(initial);
  (Harness as unknown as { value: string }).value = value;
  return (
    <ModelCombobox
      value={value}
      onValueChange={setValue}
      options={[
        { id: "deepseek-chat", name: "deepseek-chat" },
        { id: "deepseek-reasoner", name: "deepseek-reasoner" },
      ]}
      placeholder="pick a model"
    />
  );
}

describe("ModelCombobox", () => {
  beforeEach(() => {
    // The combobox closes 120ms after blur; fake timers keep that pending
    // timer deterministic so it can't fire after the jsdom environment is
    // torn down (vitest reports that as an unhandled "window is not defined").
    vi.useFakeTimers();
  });

  afterEach(() => {
    document.body.innerHTML = "";
    vi.clearAllTimers();
    vi.useRealTimers();
  });

  test("filtering the options by typed text and selecting one commits its id", async () => {
    const container = document.createElement("div");
    document.body.appendChild(container);
    const root = createRoot(container);

    await act(async () => {
      root.render(<Harness />);
    });

    const input = container.querySelector("input") as HTMLInputElement;
    expect(input).toBeTruthy();

    // Typing opens the dropdown and filters to the matching option.
    await act(async () => {
      input.focus();
    });
    await act(async () => {
      typeInto(input, "reasoner");
    });

    const items = container.querySelectorAll('[role="listbox"] button');
    expect(items.length).toBe(1);
    expect(items[0]?.textContent).toContain("deepseek-reasoner");

    // Picking the option commits the model id (not the display name).
    await act(async () => {
      items[0].dispatchEvent(new MouseEvent("mousedown", { bubbles: true }));
    });

    expect((Harness as unknown as { value: string }).value).toBe(
      "deepseek-reasoner"
    );

    await act(async () => {
      root.unmount();
    });
  });

  test("free-text fallback: typing a custom id is accepted as the value", async () => {
    const container = document.createElement("div");
    document.body.appendChild(container);
    const root = createRoot(container);

    await act(async () => {
      root.render(<Harness />);
    });

    const input = container.querySelector("input") as HTMLInputElement;
    await act(async () => {
      typeInto(input, "some/custom-model-id");
    });

    expect((Harness as unknown as { value: string }).value).toBe(
      "some/custom-model-id"
    );

    await act(async () => {
      root.unmount();
    });
  });
});
