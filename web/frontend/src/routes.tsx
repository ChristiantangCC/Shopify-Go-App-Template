import type { ComponentType } from "preact";

import { BillingPage } from "./pages/billing";
import { HomePage } from "./pages/home";
import { SettingsPage } from "./pages/settings";

/** One embedded page: a path, its component, and how the nav shows it. */
export interface Route {
  path: string;
  label: string;
  component: ComponentType;
  /** Shown in the admin sidebar only when billing is enabled. */
  requiresBilling?: boolean;
}

/**
 * The single source of truth for the app's pages. The router and the admin
 * sidebar (ui-nav-menu) both render from it, in declaration order; adding a
 * page here is all it takes to route it and surface it in the nav.
 */
export const ROUTES: Route[] = [
  { path: "/", label: "Home", component: HomePage },
  { path: "/settings", label: "Settings", component: SettingsPage },
  {
    path: "/billing",
    label: "Plans & Billing",
    component: BillingPage,
    requiresBilling: true,
  },
];
