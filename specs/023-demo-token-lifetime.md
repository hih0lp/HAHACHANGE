# SDD-023: Configurable demo token lifetime

The team directory signs JWTs with a configurable `TEAM_ACCESS_TOKEN_TTL_HOURS`. The default remains one hour when unset or invalid; the distributed demo Compose sets 8760 hours (365 days) so a chat left open for testing does not lose access after one hour. The login `expires_in` response matches the signed JWT expiry. This applies to newly issued tokens only; users refresh the page once after deployment to obtain one. The setting is limited to 1–8760 hours and must be reduced for non-demo deployments.

The React demo profile lets users select one of three identities: intern, second intern, or mentor. Selecting a role updates the demo cookie through `POST /v1/demo/role` and obtains the matching token through `POST /v1/team-access/session`. The React UI has no username or password form. The standalone onboarding page and navigation entry are removed. The scenario API remains available to existing clients.
