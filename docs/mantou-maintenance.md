# Owner-maintained gateway

This fork preserves the original license and history. Maintained source and
runtime releases belong to `j1903059993/BEpusdt`; deployment must not fetch
`v03413/BEpusdt` or its release assets. Historical attribution/import module
names are not runtime downloads. Blockchains, RPC services, toolchains and
package registries remain operational dependencies.

## Security changes

For BSC/Polygon/TRON receipt events, a database transaction serializes each
network + transaction, claims the stable event index and changes the target
order together. Claims persist independently of order deletion and do not get
released after expiration or a restart. Historical hashes without event identity
reserve the whole transaction conservatively; original order facts stay intact.
The update adds tables/columns, so back up and rehearse on a COPY of the gateway
database before replacing the dev binary. Never initialize an existing service.

EVM events use validated log indices, supported contracts on the correct network,
and successful receipt confirmation. TRON uses the complete successful execution
receipt's log array position, not calldata or a filtered loop counter. Failed,
missing and mismatched receipt facts do not confirm payments. Transfer timestamps
must be inside the original order window; a delayed scan does not make an actual
late transfer payable. Business orders that have already closed require shop-side
reconciliation; no new automatic credit/refund rule is introduced.

Signed callbacks include network, event_index and token_contract for the enabled
stablecoins. Retries require an exact success/ok merchant acknowledgement; an HTTP
200 error page is not an acknowledgement. Administrator reconciliation cannot
steal an already claimed event and still requires human verification of funds.

## Reproducible owner release

Use Go 1.26.9 and Node 22.22.3+ with pnpm 10.34.3. Work/cache/temp paths must stay
in the actual project workspace. In web/, run `corepack pnpm install
--frozen-lockfile --store-dir <workspace-cache>` and `corepack pnpm run build:prod`.
Restore only the build-generated auto-import.d.ts/components.d.ts changes if the
build regenerates them; never discard human edits. Commit the actual source.

From the clean owner checkout run:

```text
node scripts/build-owned.mjs <new-output-directory-outside-checkout>
```

The output contains an owner-built Linux binary, a manifest and commit metadata,
plus a separate complete source archive with vendored Go dependencies and compiled
static assets. Upload source/runtime/SHA256SUMS to a release in the OWNER fork;
do not upload private configuration, databases, wallets, API tokens or test logs.
The dev server receives the complete verified runtime bundle locally and does no
upstream GitHub download. Binary backups stay on the server; credentials remain
in the existing environment. Runtime and source archives are generated artifacts,
not files committed into the Git source tree.

For backend recovery, extract the source archive and use `go build -mod=vendor
./main`; static assets are present. Editing/rebuilding the admin UI still uses
its committed frontend lockfile and a normal package registry. This is zero
dependency on the original gateway repository, not a claim of zero third-party
software or zero network requirements.
