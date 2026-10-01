# Qt 6.11.2 checked Vulkan completion

`qt-6.11.2-checked-vulkan-finish.patch` is required by the opt-in Vulkan batch
renderer. It fixes Qt's ignored native idle-wait and command-buffer restart
results. Apply it to the **Qt 6.11.2 qtbase source** before building the controlled
Qt dependency:

```sh
patch --directory "$QTBASE_SOURCE" -p1 --forward \
  --input "$WHEREAMI_SOURCE/patches/qt-6.11.2-checked-vulkan-finish.patch"
```

Build Qt with the same configuration and private ABI as the rest of the SDK and
runtime. Package the corrected QtGui with the application. The patch changes a
private backend field and helper; Qt Quick, QML, platform plugins and generated
bindings must still use the matching Qt 6.11.2 build. Qt's existing source license
applies to the dependency correction. No map rendering logic is added to C++.

The loaded library exports `qt_rhi_checked_vulkan_finish_v1`. Before its first
drain, `BatchRenderer` checks that marker through a generated Qt `QLibrary`
adapter. Matching header/version strings alone do not identify a corrected runtime.
An unpatched Vulkan runtime returns `ErrUnsupportedCompletion` before executing
Planner-owned uploads. The viewer reports the error and stops.

The patch checks queue idle, command-pool reset, allocation and begin-recording
results before reporting completion. Failed producing submission, idle wait or
restart latches a context error. `endFrame` cannot record the invalid command
buffer, and subsequent `beginFrame`, `beginOffscreenFrame` and `finish` cannot
reuse it. General errors remain distinct from device loss. The finish path runs
forced retirement/readback processing only after every checked step succeeds.

The caller must stop using a failed QRhi and tear down its native owner. Recreating
only the map node/Planner on the same context cannot recover this failure. The
viewer treats `ErrNativeCompletion` as terminal; ordinary node-release/reset
events still use the existing generation-aware recreation protocol.

## Native failure tests

`cmd/vecmap-rhi/testdata/vulkan-faults.cpp` is test-only dispatch interposition.
It changes the returned native `VkResult` after the real operation succeeds, so
synthetic device loss can be tested with safe teardown on a healthy GPU. It isn't
linked into the renderer or shipped with the application.

Use a test Qt build with **`-DFEATURE_reduce_relocations=OFF`**, allowing ELF
interposition of `QVulkanDeviceFunctions`. Set its runtime library/plugin paths,
then run:

```sh
QSG_RHI_BACKEND=vulkan QT_RHI_LEAK_CHECK=1 \
sh scripts/qt-rhi-env.sh go test -race -count=1 \
  -tags 'vecmap_rhi integration' ./cmd/vecmap-rhi \
  -run '^TestNativeFinishFailures$'
```

The test compiles its interposer in `t.TempDir()` using `c++` and the matching
QtGui headers. It exercises general/device-loss results for queue wait, pool reset
and command-buffer begin: upload failure in basic/threaded loops, and retirement
failure with a basic-loop-owned Planner. Retirement stays unacknowledged and busy;
destruction does not emit another result. Qt's leak check is enabled in each child.

Check rejection separately with an unpatched runtime:

```sh
WHEREAMI_TEST_UNPATCHED_QT=1 QSG_RHI_BACKEND=vulkan \
sh scripts/qt-rhi-env.sh go test -count=1 \
  -tags 'vecmap_rhi integration' ./cmd/vecmap-rhi \
  -run '^TestUnpatchedVulkanFinish$'
```

The 2026-10-01 validation built corrected QtCore, QtGui and the XCB plugin in
temporary storage, matching Fedora's `Qt_6.11_PRIVATE_API` symbol tag, and used
the installed Qt 6.11.2 QML/Quick modules. This exercises the Linux prototype;
Flatpak verification must use the complete controlled Qt build. Physical device
loss and persistent driver failure during destruction are not induced by these
synthetic return-value tests.
