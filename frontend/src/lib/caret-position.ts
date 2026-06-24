export interface CaretPosition {
  top: number;
  left: number;
  height: number;
}

export function getCaretCoordinates(
  textarea: HTMLTextAreaElement,
  index: number
): CaretPosition {
  const style = window.getComputedStyle(textarea);
  const textareaRect = textarea.getBoundingClientRect();

  const mirror = document.createElement("div");
  mirror.style.position = "fixed";
  mirror.style.top = `${textareaRect.top}px`;
  mirror.style.left = `${textareaRect.left}px`;
  mirror.style.visibility = "hidden";
  mirror.style.whiteSpace = "pre-wrap";
  mirror.style.overflowWrap = "break-word";
  mirror.style.width = style.width;
  mirror.style.font = style.font;
  mirror.style.fontSize = style.fontSize;
  mirror.style.fontFamily = style.fontFamily;
  mirror.style.lineHeight = style.lineHeight;
  mirror.style.letterSpacing = style.letterSpacing;
  mirror.style.padding = style.padding;
  mirror.style.border = style.border;
  mirror.style.boxSizing = style.boxSizing;

  const textBefore = textarea.value.substring(0, index);
  mirror.textContent = textBefore;

  const marker = document.createElement("span");
  marker.textContent = ".";
  mirror.appendChild(marker);

  document.body.appendChild(mirror);
  const markerRect = marker.getBoundingClientRect();
  document.body.removeChild(mirror);

  return {
    top: markerRect.top,
    left: markerRect.left,
    height: markerRect.height,
  };
}
