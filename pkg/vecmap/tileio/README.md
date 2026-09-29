# tileio

Shared, Qt-free tile/glyph transport and checksum-pair disk cache mechanics,
extracted from `pkg/vecmap/tile.go`. The parent tile, raster and glyph loaders now
delegate to these helpers; decoding and preparation remain with their callers.

- `NewClient` preserves the 15-second HTTP timeout and HTTPS/same-host redirect
  policy, with fewer than ten entries in the redirect chain. Initial HTTP URLs
  remain usable for controlled local servers. A custom client owns its policy.
- `Fetch`, `Read` and `ReadFile` cap decoded response/file bytes at **2 MiB**.
  Fetch preserves Accept/User-Agent headers, returns no data for a 204 response and
  a typed `StatusError` for any other non-200 response. `StatusError.RetryAfter`
  carries a Retry-After header, capped at 24 hours. `ErrLimit` identifies
  oversized input.
- `ReadCached` reads a bounded tile and optional checksum sidecar, validates the
  sidecar's SHA-256 encoding and refreshes modification times. The caller must
  call `Verify` before trusting bytes. Explicit pinned digests bypass the sidecar.
- `WritePair` serializes tile/checksum replacement and eviction through one
  process-wide mutex, as the parent did. Files use sibling temporary files and
  rename. Relative names cannot lexically escape the cache directory. The cache
  is an **application-owned private directory**, not an untrusted filesystem or
  a cross-process transactional store.
- Eviction groups payload/sidecar paths, removes oldest groups first and preserves
  the caller's pinned entry. The default disk payload budget remains **256 MiB**.
  Pinned data may itself exceed a caller's lowered budget, as before.
- Scans now stop after **16,384 visited paths**, bounding scan-map/error cardinality.
  WritePair validates checksum size/encoding and rolls back its new pair if
  cleanup/admission fails, rather than allowing tiny responses to grow an unlimited
  cache. Loading may still use the response when optional cache admission fails.
  Filesystem operations and directory enumeration run on transport workers; runtime
  and filesystem overhead are separate from producer logical payload accounting.

`OpenFreeMap` preserves the pinned source version, canonical cache paths, and the
Madrid tile's special name/digest. Other URL templates use separate source-hashed
cache namespaces in `producer.HTTPLoader`, so their content cannot inherit that pin.

The HTTP producer adapter does no decoding. It treats 404 as missing, other 4xx
except 408/429 as permanent, and transport/5xx/408/429 failures as retryable. Oversize
and pinned checksum failures are terminal for that workload. Cached structural MVT
errors retain the parent's decode-after-checksum behavior. Dynamic provider metadata,
glyph downloads and Natural Earth raster composition remain separate work.

`producer.CacheLoader` shares the same source paths and checksum verification but
never performs a network fallback or writes a response. Missing/corrupt cache
entries fail explicitly. It is used by the viewer's `-cache-only` workload replay;
modification-time touches retain the shared cache's existing read behavior.

Headless tests cover bounded reads, cancellation, headers/statuses, redirect guards,
checksums, corruption recovery, source isolation, cache eviction and cardinality
rollback. Parent cache/raster/glyph tests continue exercising the shared helpers.
