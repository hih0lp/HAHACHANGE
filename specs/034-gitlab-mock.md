# SDD-034. GitLab mock for Bobby

The mock Compose stack sends API and worker GitLab calls to `mock-gitlab`.
The service exposes the existing 14 read-only GitLab MCP tools, authorizes
Bobby team sessions through `team-access`, and serves synthetic GitLab REST
responses over loopback. Mock mode uses no upstream GitLab origin or token.

Demo project `2` (`demo/bobby-playbook`) includes a README, branches, a tag,
a commit and diff, a merge request with notes and approvals, and a pipeline.
The authenticated demo user is the assigned reviewer, so Bobby can display
a synthetic review reminder. The mock covers Bobby's GitLab tool contract.
Repository writes and other GitLab product features are outside that contract.
