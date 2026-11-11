# SDD-023: Configurable demo token lifetime

The team directory signs JWTs with a configurable `TEAM_ACCESS_TOKEN_TTL_HOURS`. The default remains one hour when unset or invalid; the distributed demo Compose sets 8760 hours (365 days) so a chat left open for testing does not lose access after one hour. The login `expires_in` response matches the signed JWT expiry. This applies to newly issued tokens only; users refresh the page once after deployment to obtain one. The setting is limited to 1–8760 hours and must be reduced for non-demo deployments.
