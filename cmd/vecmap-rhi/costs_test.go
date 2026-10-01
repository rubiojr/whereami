//go:build vecmap_rhi

package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestParseDRMClient(t *testing.T) {
	client, ok := parseDRMClient("pos:\t0\nflags:\t02100002\ndrm-driver:\tamdgpu\ndrm-client-id:\t310\n" +
		"drm-total-vram:\t73112 KiB\ndrm-total-gtt:\t68220 KiB\ndrm-memory-vram:\t73112 KiB\n" +
		"drm-engine-gfx:\t52065383 ns\ndrm-engine-compute:\t0 ns\ndrm-engine-capacity-gfx:\t2\n")
	assert.True(t, ok)
	assert.Equal(t, "310", client.id)
	assert.Equal(t, map[string]time.Duration{"gfx": 52065383, "compute": 0}, client.engines)
	assert.Equal(t, uint64(73112), client.vramKiB)
	assert.Equal(t, uint64(68220), client.gttKiB)

	_, ok = parseDRMClient("pos:\t0\nflags:\t02\nmnt_id:\t15\n")
	assert.False(t, ok, "not a DRM descriptor")
}
