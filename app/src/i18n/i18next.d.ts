import "i18next";

import type { ptPT } from "./resources.pt-PT";

declare module "i18next" {
  interface CustomTypeOptions {
    defaultNS: "translation";
    resources: { translation: typeof ptPT };
  }
}
