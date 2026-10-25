import * as DropdownMenu from "@radix-ui/react-dropdown-menu";
import { useRef } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "./Button";

export function AccountMenu() {
  const { t } = useTranslation();
  const logoutForm = useRef<HTMLFormElement>(null);

  return (
    <>
      {/* Outside the portal: Radix unmounts the menu content when an item is selected,
          which can cancel the native form navigation after the POST reaches the BFF. */}
      <form ref={logoutForm} action="/auth/logout" method="post" className="hidden" />
      <DropdownMenu.Root modal={false}>
        <DropdownMenu.Trigger asChild>
          <Button variant="ghost" aria-label={t("auth.account")}>
            {t("auth.account")}
            <span aria-hidden="true">▾</span>
          </Button>
        </DropdownMenu.Trigger>
        <DropdownMenu.Portal>
          <DropdownMenu.Content
            align="end"
            sideOffset={8}
            className="z-50 min-w-48 rounded-xl border border-line bg-surface p-1 shadow-lg"
          >
            <DropdownMenu.Item
              className="min-h-12 w-full rounded-lg px-3 text-left text-sm text-fg outline-none hover:bg-surface-2 focus:bg-surface-2"
              onSelect={() => logoutForm.current?.requestSubmit()}
            >
              {t("auth.logout")}
            </DropdownMenu.Item>
          </DropdownMenu.Content>
        </DropdownMenu.Portal>
      </DropdownMenu.Root>
    </>
  );
}
