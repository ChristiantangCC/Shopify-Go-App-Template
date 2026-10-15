// Globals the two CDN scripts in index.html provide: the App Bridge API on
// window.shopify and the Polaris web components as custom elements.

declare global {
  interface ShopifyGlobal {
    /** Mints a fresh session token (JWT) for the current user and shop. */
    idToken: () => Promise<string>;
    config: {
      shop?: string;
      locale?: string;
      apiKey?: string;
    };
    toast: {
      show: (message: string, opts?: { isError?: boolean; duration?: number }) => void;
    };
  }

  const shopify: ShopifyGlobal;
}

declare module 'preact' {
  namespace JSX {
    interface IntrinsicElements {
      /** Polaris web components (s-page, s-section, s-button, ...). */
      [tagName: `s-${string}`]: HTMLAttributes<HTMLElement> & Record<string, unknown>;
      /** App Bridge nav menu: its anchors become the admin sidebar. */
      'ui-nav-menu': HTMLAttributes<HTMLElement>;
      /** App Bridge contextual title bar. */
      'ui-title-bar': HTMLAttributes<HTMLElement> & { title?: string };
    }
  }
}

export {};
