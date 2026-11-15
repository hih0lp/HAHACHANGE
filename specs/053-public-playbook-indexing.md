# SDD-053: Public AvitoTech Playbook in the local RAG index

The local test stack mounts the sibling checkout of `https://github.com/avito-tech/playbook` at `/data/knowledge/playbook` in the API container. The existing Markdown walker recursively indexes its `.md` files together with `fixtures/knowledge`. The checkout is read-only inside the container; `.git` is skipped by the walker. The API reindexes on startup by default, and `POST /v1/knowledge/reindex` refreshes the collection after a source update without rebuilding the image.

The server checkout must contain the intended public repository and revision. A missing source directory is a deployment error: Docker may otherwise create an empty bind-mount directory. Verify the checkout and document count before recreating the API. This mount applies to the `bobby-local` test stack; the distributed Compose file already mounts the same sibling Playbook checkout.

Acceptance: the API container lists the Playbook Markdown files, the reindex result counts them alongside local knowledge files, and a search for a distinctive Playbook topic returns a `playbook/` path. Preserve the other local Compose overlays and existing mock JSON files during deployment.
