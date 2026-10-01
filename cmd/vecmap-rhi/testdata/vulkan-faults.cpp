// Test-only interposition at Qt's native Vulkan dispatch boundary.
// Each operation really completes before its return value is replaced, allowing
// synthetic device-loss tests to tear down a healthy driver safely.
#include <QVulkanFunctions>
#include <atomic>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <dlfcn.h>

static std::atomic<int> waits{0};
static std::atomic<bool> armed{false};

template<typename F> static F next(const char *name)
{
    auto function = reinterpret_cast<F>(dlsym(RTLD_NEXT, name));
    if (!function)
        std::abort();
    return function;
}

static VkResult inject(const char *operation, VkResult result)
{
    const char *selected = std::getenv("WHEREAMI_QRHI_FAILURE");
    const char *replacement = std::getenv("WHEREAMI_QRHI_FAILURE_RESULT");
    if (result != VK_SUCCESS || !selected || !replacement || std::strcmp(selected, operation) || !armed.exchange(false))
        return result;
    result = static_cast<VkResult>(std::atoi(replacement));
    std::fprintf(stderr, "injected_native_failure operation=%s result=%d\n", operation, int(result));
    return result;
}

VkResult QVulkanDeviceFunctions::vkQueueWaitIdle(VkQueue queue)
{
    using F = VkResult (*)(QVulkanDeviceFunctions *, VkQueue);
    static auto function = next<F>("_ZN22QVulkanDeviceFunctions15vkQueueWaitIdleEP9VkQueue_T");
    const VkResult result = function(this, queue);
    // Two startup drains precede the first upload; retirement is the fourth.
    const char *phase = std::getenv("WHEREAMI_QRHI_FAILURE_PHASE");
    if (++waits == (phase && !std::strcmp(phase, "release") ? 4 : 3))
        armed = true;
    return inject("wait", result);
}

VkResult QVulkanDeviceFunctions::vkResetCommandPool(VkDevice device, VkCommandPool pool, VkCommandPoolResetFlags flags)
{
    using F = VkResult (*)(QVulkanDeviceFunctions *, VkDevice, VkCommandPool, VkCommandPoolResetFlags);
    static auto function = next<F>("_ZN22QVulkanDeviceFunctions18vkResetCommandPoolEP10VkDevice_TP15VkCommandPool_Tj");
    return inject("reset", function(this, device, pool, flags));
}

VkResult QVulkanDeviceFunctions::vkBeginCommandBuffer(VkCommandBuffer buffer, const VkCommandBufferBeginInfo *info)
{
    using F = VkResult (*)(QVulkanDeviceFunctions *, VkCommandBuffer, const VkCommandBufferBeginInfo *);
    static auto function = next<F>("_ZN22QVulkanDeviceFunctions20vkBeginCommandBufferEP17VkCommandBuffer_TPK24VkCommandBufferBeginInfo");
    return inject("begin", function(this, buffer, info));
}
