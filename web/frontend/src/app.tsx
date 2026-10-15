import { useQuery } from '@tanstack/react-query';
import { LocationProvider, Route, Router } from 'preact-iso';

import { bootstrapSession } from './lib/auth-fetch';
import { ROUTES } from './routes';

/**
 * Bootstraps the session (install/token exchange happen server-side when
 * needed), then renders the nav and the routed page.
 */
export const App = () => {
  const session = useQuery({
    queryKey: ['session'],
    queryFn: bootstrapSession,
    staleTime: Infinity,
    retry: 1,
  });

  if (session.isError) {
    return (
      <s-page>
        <s-banner tone="critical" heading="The app could not start">
          Reopen it from the Shopify admin. If this keeps happening, check the server logs.
        </s-banner>
      </s-page>
    );
  }
  if (!session.data) {
    return (
      <s-page>
        <s-spinner accessibilityLabel="Loading" size="large"></s-spinner>
      </s-page>
    );
  }

  const routes = ROUTES.filter((route) => !route.requiresBilling || session.data.billingEnabled);
  return (
    <LocationProvider>
      <ui-nav-menu>
        {routes.map((route) => (
          <a href={route.path} rel={route.path === '/' ? 'home' : undefined}>
            {route.label}
          </a>
        ))}
      </ui-nav-menu>
      <Router>
        {routes.map((route) => (
          <Route path={route.path} component={route.component} />
        ))}
      </Router>
    </LocationProvider>
  );
};
