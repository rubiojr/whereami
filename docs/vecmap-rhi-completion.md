# Asynchronous QRhi completion: Qt 6.11.2 audit

Checkpoint: **w47r**, following checked submission commit **07301d5**.

## Decision

Keep stock Qt and the checked producing-frame submission path. Qt 6.11.2's
readback callback cannot establish successful submission, safe cancellation and
completed retirement together. The audit resolves the proposed callback-only
replacement; a future asynchronous executor needs the integration boundary and
acceptance tests below.

Review on **2026-10-01** also found an error-reporting limit in the checked Vulkan
path: `QRhiVulkan::finish` ignores idle-wait and command-buffer restart results.
Follow-up **1aaq** tracks exposing those failures before acknowledging native
completion. Checking the public `finish` result is necessary, but does not cover
errors that Qt discards internally.

The current checked in-frame `finish()` path remains the correctness baseline.
Replacing it with `QRhiReadbackResult::completed` alone does not preserve its
submission or teardown guarantees. This audit identifies the missing guarantees;
it does not implement an asynchronous executor or claim one is impossible with
additional native integration.

## What completion must establish

The adapter has three separate obligations:

1. **Upload publication:** the producing command buffer was successfully submitted,
   and all uploaded versions are usable by the next selected frame.
2. **Retirement:** retired allocations have completed native lifetime obligations
   before the Planner reclaims capacity. A scheduled `DeleteLater` is insufficient.
3. **Cancellation:** dropping a renderer or generation suppresses old notifications
   without freeing storage that Qt can still write. Native result storage and Go
   callback captures have different lifetimes.

A healthy device, elapsed frame count, or callback invocation does not establish
all three.

## Source findings

All links below refer to the **v6.11.2** source tag. These are source-path findings,
not injected GPU-failure test results.

### Vulkan readback callbacks do not report submission success

In [qrhivulkan.cpp](https://github.com/qt/qtbase/blob/v6.11.2/src/gui/rhi/qrhivulkan.cpp):

- Recording a static-buffer or texture readback adds an entry to
  `activeBufferReadbacks` or `activeTextureReadbacks`, before queue submission.
- `endFrame` returns immediately when `endAndSubmitPrimaryCommandBuffer` fails.
  It sets `frame.cmdFenceWaitable = true` only after successful submission. A
  non-device-loss `vkQueueSubmit` error returns `FrameOpError` without setting
  `deviceLost`.
- If a subsequent `beginFrame` succeeds on that same slot,
  `waitCommandCompletion` skips a fence that is not waitable. `prepareNewFrame`
  then calls `finishActiveReadbacks`.
- `finishActiveReadbacks` selects entries by frame-slot equality (or `forced`),
  copies staging memory, and invokes callbacks. It does not check whether each
  readback's producing command buffer was successfully submitted.
- `destroy` also calls `finishActiveReadbacks(true)`, including after device loss.
  It skips `vkDeviceWaitIdle` when `deviceLost` is set.

Thus `completed` can be delivered on a path that did not establish successful
submission. A nonempty result is not enough either: the staging allocation may be
mapped successfully even if the GPU copy was never submitted. A marker scheme
would need its own proof of provenance and ordering; reusing a constant marker or
merely checking data length is insufficient.

Dynamic-buffer readback is an additional trap: the Vulkan implementation can copy
host-visible data and invoke its callback directly during resource-update handling.
It is not a GPU fence for preceding static-buffer or texture uploads.

### Qt Quick does not expose the required result in its frame signals

The [threaded loop](https://github.com/qt/qtdeclarative/blob/v6.11.2/src/quick/scenegraph/qsgthreadedrenderloop.cpp)
calls `fireFrameSwapped()` after handling an unsuccessful `endFrame` too.
`frameSwapped` is therefore not a substitute for the already-rejected
`afterFrameEnd` success test. The latter also fires on failed `beginFrame`.

The current binding surface borrows Qt Quick's QRhi and command buffer; it does
not own `beginFrame`/`endFrame`. Generating another signal binding cannot recover
the missing result.

### QRhi cleanup runs before the last possible result write

In [qrhi.cpp](https://github.com/qt/qtbase/blob/v6.11.2/src/gui/rhi/qrhi.cpp),
`QRhi::~QRhi` performs these operations in order:

1. `d->runCleanup()`; this invokes and clears registered cleanup callbacks.
2. Delete pending `QRhiResource` wrappers.
3. `d->destroy()`.
4. Delete the backend implementation.

Vulkan's step 3 can still write pending readback results and invoke `completed`.
Freeing a result in `addCleanupCallback` therefore risks use-after-free. Capturing
the result in a shared owner held only by that cleanup registration is also
insufficient: the registrations are cleared before backend destruction.

Disconnecting a Go observer may release its captures, but cannot cancel Qt's raw
result pointer. Conversely, retaining a canceled result forever avoids that access
error at the cost of an unbounded leak across resets. A valid design needs an
explicit final-native-reference boundary, including requests that never callback.

### OpenGL readback can block and can be abandoned

In [qrhigles2.cpp](https://github.com/qt/qtbase/blob/v6.11.2/src/gui/rhi/qrhigles2.cpp),
command execution uses `glReadPixels`, `glGetBufferSubData`, or a mapped-buffer
read, then invokes `completed` inline. Changing the Go callback API does not make
these driver calls nonblocking.

`endFrame` can return on an `ensureContext` failure before executing the command
buffer. Its readback callback is then not delivered by that call. Cleanup must
handle abandoned requests as well as successful callbacks.

### Upload completion and released capacity remain distinct

`QRhi::endFrame` calls the backend first, then deletes pending `DeleteLater`
wrappers. An OpenGL readback callback executes inside that backend call, before
those wrappers are deleted. Vulkan also has a separate deferred-release queue.
An upload marker callback must not automatically acknowledge a release batch.

### The checked Vulkan path has an error-reporting boundary too

In [QRhiVulkan::finish](https://github.com/qt/qtbase/blob/v6.11.2/src/gui/rhi/qrhivulkan.cpp#L3089-L3134):

- The producing in-frame `endAndSubmitPrimaryCommandBuffer` result is checked and
  returned on failure. This is the submission check used by `BatchRenderer`.
- The following `vkQueueWaitIdle` result is discarded. This call does not update
  Qt's `deviceLost` flag when the wait fails.
- The calls to `startPrimaryCommandBuffer` that restart frame recording also have
  their results discarded. A general restart failure need not set `deviceLost`.
- Execution then reaches forced deferred release/readback processing and returns
  `FrameOpSuccess` even if one of those unchecked operations failed.

`BatchRenderer.drain` checks the public result and `IsDeviceLost`, so reported
errors reset the namespace. It cannot observe a native error that neither check
exposes. The checked path therefore establishes reported submission success; its
completion/retirement proof also depends on the native idle wait succeeding.
Current tests do not prove behavior when Qt hides a wait or restart failure.

**1aaq** requires a version-pinned Qt correction or generated backend integration
that exposes those results with valid submission and lifetime ordering. A failed
wait must not be treated as completed retirement, and a failed restart must not
leave the adapter acknowledging a healthy namespace. Adding a later queue wait
alone does not fix the producing-submission or command-buffer restart boundary.

## Implementation boundary to choose

The stock-Qt baseline can continue supporting live retained targets while keeping
checked submission and measured retirement drains. No API change is needed for
that work.

For the original performance goal, the recommended sequence is **live targets
first**, followed by workload measurements, then backend-specific completion if
the drain costs justify it. Live fixture replacement (**w8sf**) and the live
tile/placement Bridge (**hxzf**) now use that checked path. The recorded
[accepted-target latency replays](vecmap-rhi.md#bounded-live-target-supersession) in **3a38** reduced
moving Vulkan settlement from 14.55 s to 6.96–7.37 s by superseding serial targets,
with every drain retained. Drain wall totals were 141.63–160.43 ms, against
340.43 ms in the baseline. These are historical input/settings-matched replays,
not equal-intermediate-work comparisons or presentation measurements. They support
measuring scheduling and workload costs before choosing an asynchronous boundary;
they do not resolve the hidden native failures tracked by **1aaq**.

Removing those drains requires a separately scoped integration, for example:

- **Backend-specific completion:** use generated native API bindings, keep all
  scheduling/ownership policy in Go, and prove submission-associated completion,
  failure cancellation, retirement, and teardown for each supported backend.
  Unsupported backends retain the checked fallback. A later unrelated queue fence
  alone cannot prove that an earlier failed submission ran.
- **Version-pinned Qt integration:** expose the producing frame's submission result
  and a reliable final-reference/cancellation boundary, or own the render loop.
  This changes the Qt build/distribution or window integration boundary and needs
  a dedicated design before implementation.

Both routes require more than a readback binding. An unused raw-pointer callback
API would leave the ownership problem unresolved.

The required boundary must expose all of these independently:

| Obligation | Required observation |
| --- | --- |
| Upload publication | Result of the exact producing submission, including general errors on a healthy device. |
| Native completion | Checked fence/wait result; the restarted command buffer must also be valid. |
| Retirement capacity | Wrapper deletion and completed backend retirement, after the frame boundary. |
| Cancellation/teardown | Final native access to each result, including abandoned requests and callbacks during backend destruction. |

An owner generation suppresses stale Go delivery; it does not cancel Qt's raw
result pointer. Cleanup must retain native storage until that final-access
boundary, while releasing canceled Go captures without an unbounded tombstone list.

## Acceptance tests for an asynchronous replacement

- Successful partial uploads preserve old pixels and publish only after the exact
  producing operation is proven; activation does not reupload resources.
- A general submission error on a healthy device never yields a successful ack,
  even if a readback callback subsequently fires with nonempty data.
- Native idle-wait and command-buffer restart failures cannot produce a successful
  completion acknowledgement or reclaim retirement capacity (**1aaq**).
- Immediate, delayed, and absent callbacks all have bounded storage ownership.
- Cancel before recording, after recording, after submission, and during delivery;
  then recreate repeatedly on the same context and on a replacement context.
- Context teardown may invoke late callbacks; no Go captures or native result
  storage leak, and no result storage is freed before the last native access.
- Release acknowledgements follow completed native retirement, independently of
  upload-marker readiness.
- Exercise basic/threaded loops and supported backend fallbacks. Measure driver
  call latency as well as Go callback latency before claiming nonblocking work.

The existing native tests were rerun before commit 07301d5 and passed on Vulkan,
including binding reproduction and basic/threaded viewer processes. They inject
finish return values, not failed Vulkan queue submissions or physical device loss;
they do not experimentally verify the readback failure paths audited here.

## Review verification: 2026-10-01

Rechecked the v6.11.2 QRhi, Vulkan, OpenGL and threaded-render-loop source against
`internal/vecmaprhi/batch.go` and the generated callback ownership contract.

The installed Qt is **6.11.2-2.fc44**, with Mesa **26.2.3** and Go **1.27.1**.
The matching `qt6-qtbase-private-devel` RPM was extracted under
`/tmp/opencode/whereami-qt-audit`; `QT_RHI_INCLUDE` points to its
`usr/include/qt6/QtGui/6.11.2/QtGui` directory. Verification passed:

- Vulkan `make rhi-test`, including byte-for-byte binding regeneration and the
  native batch executor's upload, rollback, retirement and reported-failure tests.
- Fresh Vulkan basic/threaded viewer lifecycle, reload and live-transition
  processes on **AMD Radeon 860M Graphics (RADV KRACKAN1)**.
- The complete bindings, adapter and viewer suites with the race detector,
  `GOAMD64=v1`, OpenGL under Xvfb and `QT_SCALE_FACTOR=2`. Adapter statement
  coverage was **91.3%**; viewer subprocess coverage is separate from the parent.
- Tagged staticcheck for those three packages and `git diff --check`.

Native runs enabled `QT_RHI_LEAK_CHECK=1`. These verify the retained checked path
on working drivers and injected public return values. They do not inject failed
Vulkan submissions, idle waits, command-buffer restarts or asynchronous readback
teardown. Those failure cases remain explicit integration requirements.
