import type { AnchorHTMLAttributes, MouseEvent, ReactNode } from "react";
import { useNavigate } from "react-router-dom";
import { resolvePath } from "@/react/router/route-index";

export type RouterLinkProps = Omit<
  AnchorHTMLAttributes<HTMLAnchorElement>,
  "href"
> & {
  name?: string;
  path?: string;
  params?: Record<string, string | string[] | undefined>;
  children?: ReactNode;
};

export function RouterLink({
  name,
  path,
  params,
  children,
  onClick,
  target,
  download,
  ...props
}: RouterLinkProps) {
  const navigate = useNavigate();
  const href = name ? resolvePath(name, params) : (path ?? "/");

  const handleClick = (event: MouseEvent<HTMLAnchorElement>) => {
    onClick?.(event);
    if (
      event.defaultPrevented ||
      event.metaKey ||
      event.ctrlKey ||
      event.shiftKey ||
      event.altKey ||
      event.button !== 0 ||
      (target && target !== "_self") ||
      download != null
    ) {
      return;
    }
    event.preventDefault();
    navigate(href, { replace: false });
  };

  return (
    <a
      {...props}
      href={href}
      target={target}
      download={download}
      onClick={handleClick}
    >
      {children}
    </a>
  );
}
