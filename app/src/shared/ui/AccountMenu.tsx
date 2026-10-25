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
          <Button variant="ghost" size="compact" aria-label={t("auth.account")}>
            {t("auth.account")}
            <span aria-hidden="true">▾</span>
          </Button>
        </DropdownMenu.Trigger>
        <DropdownMenu.Portal>
          <DropdownMenu.Content
            align="end"
            sideOffset={8}
            className="z-50 min-w-48 rounded-lg border border-pitbox-line bg-white p-1 shadow-lg"
          >
            <DropdownMenu.Item
              className="w-full rounded-md px-3 py-2 text-left text-sm text-pitbox-ink outline-none hover:bg-slate-100 focus:bg-slate-100"
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
