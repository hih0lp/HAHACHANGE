# SDD-031: prompt-injection boundaries in Bobby

## Problem and scope

Bobby consumes document fragments, team profile data, form descriptions and MCP results that may contain text written by someone other than the current user. Previously the team profile and full MCP catalogue were concatenated into the system message, and document fragments were appended as plain text beside the question. A malicious source could mimic a role delimiter or instruction. The current user request remains authoritative for intent; retrieved content must remain evidence only.

## Behaviour

The system message contains only application-authored rules, the current server clock and static tool-use instructions. It explicitly tells the model to ignore role, policy, tool-selection, secret-disclosure and action commands inside source data, tool output, catalogue text and previous answers. It does not claim that a prompt alone guarantees immunity.

The user question and document hits are serialized as a JSON envelope with separate `question`, `document_search` and `document_fragments` fields. Team profile data is parsed as JSON, stripped of unusable `jira_profile_url` fields and placed in the lower-trust request data; malformed profile text is omitted. The MCP tool catalogue is also placed in request data rather than the system message. Native function tools still come from the actual MCP `tools/list` response, and a model tool call is accepted only if its name exists in the server's current endpoint map. MCP result content remains data in tool messages or JSON-encoded fallback messages. Only the separate form confirmation endpoint can submit a form; advertised chat tools do not include that endpoint.

The planner receives its own static system instruction and a JSON user payload with `question`, `current_time` and `available_forms`. It is told that form titles and descriptions are untrusted catalogue data and cannot override its classification rules. The planner continues to choose Jira, Calendar, team and form evidence semantically without fixed issue keys or phrase matching. The JSON fixture format and mock-only integration rules remain as specified in SDD-029 and SDD-030.

## Verification

Unit tests inject role-delimiter and instruction text into a document fragment, team profile and form description. They assert that those bytes remain in data fields and never enter system instructions, while legitimate evidence survives. The document test also supplies a non-finite retrieval score to prove that incidental ranking metadata cannot break the JSON envelope. Run `go test ./...`, then rebuild only the isolated `bobby-mocks` test stack and check chat, team, form, Jira and Calendar requests. No production `.env` may be read or copied. This boundary reduces prompt-injection exposure; model behaviour must still be checked on adversarial questions because no textual rule can provide an absolute guarantee.

Local verification on 14 November 2026: targeted prompt-boundary and existing chat/planner tests passed; the full `go test -count=1 -cover ./...` run passed before the final score-serialization guard, and targeted tests passed again after it. The server package measured 43.0% statement coverage in the full run. The isolated server deployment and smoke checks are tracked below when complete.
