import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

// navigator.clipboard only exists in secure contexts (HTTPS or localhost);
// self-hosted dashboards are often opened over plain HTTP, so fall back to execCommand.
export async function copyText(text: string) {
  if (navigator.clipboard && window.isSecureContext) {
    return navigator.clipboard.writeText(text);
  }
  const active = document.activeElement as HTMLElement | null;
  const el = document.createElement("textarea");
  el.value = text;
  el.setAttribute("readonly", "");
  el.style.position = "fixed";
  el.style.opacity = "0";
  // Mount next to the focused element so a dialog's focus trap doesn't block select().
  (active?.parentElement ?? document.body).appendChild(el);
  el.select();
  const ok = document.execCommand("copy");
  el.remove();
  active?.focus();
  if (!ok) throw new Error("Copy failed");
}
