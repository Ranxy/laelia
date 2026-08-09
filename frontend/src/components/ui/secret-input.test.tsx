import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

// SecretInput uses react-i18next via useTranslation; stub it so the test does
// not depend on the i18n provider.
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

import { SecretInput } from "./secret-input";

const MASK_CLASS = "[-webkit-text-security:disc]";

describe("SecretInput", () => {
  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("renders a plain text input, not a password field", () => {
    render(<SecretInput placeholder="api key" />);
    const input = screen.getByPlaceholderText("api key");
    expect(input.tagName).toBe("INPUT");
    expect(input.getAttribute("type")).toBe("text");
  });

  it("disables browser autofill heuristics", () => {
    render(<SecretInput placeholder="api key" />);
    const input = screen.getByPlaceholderText("api key");
    expect(input.getAttribute("autocomplete")).toBe("off");
    expect(input.getAttribute("autocorrect")).toBe("off");
    expect(input.getAttribute("autocapitalize")).toBe("off");
    expect(input.getAttribute("spellcheck")).toBe("false");
    expect(input.getAttribute("data-1p-ignore")).not.toBeNull();
    expect(input.getAttribute("data-lpignore")).toBe("true");
    expect(input.getAttribute("data-form-type")).toBe("other");
  });

  it("masks by default and reveals on toggle", () => {
    render(<SecretInput placeholder="api key" />);
    const input = screen.getByPlaceholderText("api key");
    expect(input.className).toContain(MASK_CLASS);
    fireEvent.click(screen.getByRole("button"));
    expect(input.className).not.toContain(MASK_CLASS);
    fireEvent.click(screen.getByRole("button"));
    expect(input.className).toContain(MASK_CLASS);
  });

  it("forwards value, onChange and id", () => {
    const onChange = vi.fn();
    render(
      <SecretInput
        id="secret"
        value="sk-123"
        onChange={onChange}
        placeholder="api key"
      />
    );
    const input = screen.getByPlaceholderText("api key");
    expect(input.id).toBe("secret");
    expect(input).toHaveValue("sk-123");
    fireEvent.change(input, { target: { value: "sk-456" } });
    expect(onChange).toHaveBeenCalled();
  });
});
