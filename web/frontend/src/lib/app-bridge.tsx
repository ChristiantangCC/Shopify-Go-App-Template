import { h, type ComponentChildren } from 'preact';

// App Bridge types its elements for the global JSX namespace only, which
// Preact's JSX doesn't read, so the elements this app uses are created here
// with h(). Everything else comes typed from @shopify/polaris-types and
// @shopify/app-bridge-types (tsconfig "types").

/** The admin sidebar: its anchors become the app's nav items. */
export const NavMenu = ({ children }: { children: ComponentChildren }) => h('ui-nav-menu', null, children);
