import { Slider as BaseSlider } from "@base-ui/react/slider";
import type { ComponentPropsWithoutRef } from "react";
import { cn } from "@/lib/utils";

// Single-thumb horizontal slider matching the Input control look: track in
// control-bg, filled range and thumb in accent. Base UI positions the
// indicator and thumb inline; keyboard focus lands on the thumb's hidden
// range input, surfaced here through has-[input:focus-visible].
type BaseSliderProps = ComponentPropsWithoutRef<typeof BaseSlider.Root>;

interface SliderProps
  extends Omit<
    BaseSliderProps,
    "value" | "defaultValue" | "onValueChange" | "onValueCommitted"
  > {
  value?: number;
  defaultValue?: number;
  onValueChange?: (value: number) => void;
  /** Accessible name for the thumb when no visible Slider.Label is rendered. */
  "aria-label"?: string;
}

function Slider({
  className,
  "aria-label": ariaLabel,
  ...rootProps
}: SliderProps) {
  return (
    <BaseSlider.Root
      {...rootProps}
      className={cn(
        "flex w-full touch-none items-center select-none",
        className
      )}
    >
      <BaseSlider.Control className="flex h-5 w-full items-center">
        <BaseSlider.Track className="relative h-1.5 w-full rounded-full bg-control-bg">
          <BaseSlider.Indicator className="rounded-full bg-accent" />
          <BaseSlider.Thumb
            aria-label={ariaLabel}
            className={cn(
              "size-4 rounded-full border-2 border-accent bg-background shadow-sm",
              "focus:outline-hidden has-[input:focus-visible]:ring-2 has-[input:focus-visible]:ring-accent has-[input:focus-visible]:ring-offset-2"
            )}
          />
        </BaseSlider.Track>
      </BaseSlider.Control>
    </BaseSlider.Root>
  );
}

export type { SliderProps };
export { Slider };
