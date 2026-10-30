import { forwardRef, type ComponentPropsWithoutRef } from "react";

import { TextInput } from "./TextInput";

type Props = Omit<ComponentPropsWithoutRef<"input">, "type"> & {
  /** Accessible name; a search field has no visible label. */
  label: string;
};

export const SearchInput = forwardRef<HTMLInputElement, Props>(
  ({ label, ...props }, ref) => (
    <TextInput ref={ref} type="search" aria-label={label} enterKeyHint="search" {...props} />
  ),
);
SearchInput.displayName = "SearchInput";
