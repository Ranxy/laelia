import { act, useState } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, describe, expect, test } from "vitest";
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

// The popup renders through a portal into the shared overlay layer root, so
// its options never live inside the trigger's container.
function popupItems(): HTMLElement[] {
  return [...document.querySelectorAll<HTMLElement>('[role="listbox"] button')];
}

function isPopupOpen(): boolean {
  return document.querySelector('[role="listbox"]') !== null;
}

describe("ModelCombobox", () => {
  afterEach(() => {
    document.body.innerHTML = "";
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

    const items = popupItems();
    expect(items.length).toBe(1);
    expect(items[0]?.textContent).toContain("deepseek-reasoner");

    // Picking the option commits the model id (not the display name).
    await act(async () => {
      items[0].dispatchEvent(new MouseEvent("mousedown", { bubbles: true }));
    });

    expect((Harness as unknown as { value: string }).value).toBe(
      "deepseek-reasoner"
    );
    expect(isPopupOpen()).toBe(false);

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

  test("a pointerdown outside the trigger and popup closes the dropdown", async () => {
    const container = document.createElement("div");
    document.body.appendChild(container);
    const root = createRoot(container);

    await act(async () => {
      root.render(<Harness />);
    });

    const input = container.querySelector("input") as HTMLInputElement;
    await act(async () => {
      input.focus();
    });
    expect(isPopupOpen()).toBe(true);

    // Pressing inside the popup does not close it.
    await act(async () => {
      document
        .querySelector('[role="listbox"]')
        ?.dispatchEvent(
          new MouseEvent("pointerdown", { bubbles: true, composed: true })
        );
    });
    expect(isPopupOpen()).toBe(true);

    // Pressing on the trigger still does not close it.
    await act(async () => {
      input.dispatchEvent(new MouseEvent("pointerdown", { bubbles: true }));
    });
    expect(isPopupOpen()).toBe(true);

    // Pressing anywhere else closes it.
    await act(async () => {
      document.body.dispatchEvent(
        new MouseEvent("pointerdown", { bubbles: true })
      );
    });
    expect(isPopupOpen()).toBe(false);

    await act(async () => {
      root.unmount();
    });
  });

  test("escape closes the dropdown without changing the value", async () => {
    const container = document.createElement("div");
    document.body.appendChild(container);
    const root = createRoot(container);

    await act(async () => {
      root.render(<Harness />);
    });

    const input = container.querySelector("input") as HTMLInputElement;
    await act(async () => {
      input.focus();
    });
    await act(async () => {
      typeInto(input, "chat");
    });
    expect(popupItems().length).toBe(1);

    await act(async () => {
      input.dispatchEvent(
        new KeyboardEvent("keydown", { key: "Escape", bubbles: true })
      );
    });
    expect(isPopupOpen()).toBe(false);
    expect((Harness as unknown as { value: string }).value).toBe("chat");

    await act(async () => {
      root.unmount();
    });
  });

  test("keyboard navigation still picks the highlighted option with Enter", async () => {
    const container = document.createElement("div");
    document.body.appendChild(container);
    const root = createRoot(container);

    await act(async () => {
      root.render(<Harness />);
    });

    const input = container.querySelector("input") as HTMLInputElement;
    await act(async () => {
      input.focus();
    });
    await act(async () => {
      input.dispatchEvent(
        new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true })
      );
    });
    expect(isPopupOpen()).toBe(true);

    await act(async () => {
      input.dispatchEvent(
        new KeyboardEvent("keydown", { key: "Enter", bubbles: true })
      );
    });

    expect((Harness as unknown as { value: string }).value).toBe(
      "deepseek-chat"
    );
    expect(isPopupOpen()).toBe(false);

    await act(async () => {
      root.unmount();
    });
  });
});
