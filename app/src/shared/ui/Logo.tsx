import { cn } from "@/shared/lib/cn";

export function Logo({ size = "sm" }: { size?: "sm" | "lg" }) {
  const large = size === "lg";
  return (
    <span className="inline-flex items-center gap-3">
      <span
        className={cn(
          "flex items-center justify-center bg-primary text-on-primary",
          large ? "size-16 rounded-2xl" : "size-10 rounded-xl",
        )}
      >
        <svg
          aria-hidden="true"
          className={large ? "size-8" : "size-5"}
          viewBox="0 0 32 32"
          fill="none"
        >
          <circle cx="16" cy="16" r="11" stroke="currentColor" strokeWidth="2" />
          <path d="M16 16 22 9" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
          <circle cx="16" cy="16" r="2" fill="currentColor" />
        </svg>
      </span>
      <span className={cn("font-black tracking-tight text-fg", large ? "text-3xl" : "text-lg")}>
        Pit<span className="text-primary">Box</span>
      </span>
    </span>
  );
}
