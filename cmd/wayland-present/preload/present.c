// Benchmark tooling, never linked into an application: an LD_PRELOAD library
// that requests wp_presentation feedback for every wl_surface commit of a native
// Wayland client and logs the compositor's answers. cmd/wayland-present builds
// it with the protocol code wayland-scanner generates, runs the client and reads
// the log. The client's own requests are forwarded unchanged.

#define _GNU_SOURCE
#include <dlfcn.h>
#include <pthread.h>
#include <stdarg.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <wayland-client.h>

#include "presentation-time-client-protocol.h"

// libwayland's WL_CLOSURE_MAX_ARGS, which its public headers don't export.
#define MAX_ARGS 20
#define MAX_SURFACES 64

typedef struct wl_proxy *(*marshal_array_flags_fn)(struct wl_proxy *, uint32_t,
						   const struct wl_interface *,
						   uint32_t, uint32_t,
						   union wl_argument *);
typedef struct wl_display *(*connect_fn)(const char *);
typedef struct wl_display *(*connect_to_fd_fn)(int);

static marshal_array_flags_fn real_marshal_array_flags;
static connect_fn real_connect;
static connect_to_fd_fn real_connect_to_fd;
static pthread_once_t resolved = PTHREAD_ONCE_INIT;

// Written once while the first display connects, before the client commits.
static struct wl_display *display;
static struct wl_event_queue *queue;
static struct wp_presentation *presentation;
static clockid_t clock_id = CLOCK_MONOTONIC;

static pthread_mutex_t log_lock = PTHREAD_MUTEX_INITIALIZER;
static FILE *out;

// Surfaces with a buffer attached since their last commit. Commits without one
// carry no new content and are left alone. With Vulkan on mutter every buffer
// commit is followed by such a state-only commit, which mutter reports discarded.
static pthread_mutex_t surfaces_lock = PTHREAD_MUTEX_INITIALIZER;
static uint32_t attached[MAX_SURFACES];
static int attached_count;

struct commit {
	uint32_t surface;
	uint64_t at;
};

static void resolve(void)
{
	real_marshal_array_flags = (marshal_array_flags_fn)dlsym(RTLD_NEXT, "wl_proxy_marshal_array_flags");
	real_connect = (connect_fn)dlsym(RTLD_NEXT, "wl_display_connect");
	real_connect_to_fd = (connect_to_fd_fn)dlsym(RTLD_NEXT, "wl_display_connect_to_fd");
	if (!real_marshal_array_flags || !real_connect || !real_connect_to_fd) {
		fprintf(stderr, "wayland-present: libwayland-client 1.20 or newer is required\n");
		abort();
	}
}

static uint64_t now(void)
{
	struct timespec ts;
	clock_gettime(clock_id, &ts);
	return (uint64_t)ts.tv_sec * 1000000000u + (uint64_t)ts.tv_nsec;
}

static void record(const char *format, ...)
{
	va_list ap;
	pthread_mutex_lock(&log_lock);
	va_start(ap, format);
	vfprintf(out, format, ap);
	va_end(ap);
	pthread_mutex_unlock(&log_lock);
}

static void feedback_sync_output(void *data, struct wp_presentation_feedback *feedback,
				 struct wl_output *output)
{
}

static void feedback_presented(void *data, struct wp_presentation_feedback *feedback,
			       uint32_t tv_sec_hi, uint32_t tv_sec_lo, uint32_t tv_nsec,
			       uint32_t refresh, uint32_t seq_hi, uint32_t seq_lo,
			       uint32_t flags)
{
	struct commit *commit = data;
	uint64_t at = (((uint64_t)tv_sec_hi << 32) | tv_sec_lo) * 1000000000u + tv_nsec;
	uint64_t seq = ((uint64_t)seq_hi << 32) | seq_lo;
	record("presented %u %llu %llu %u %llu %u\n", commit->surface,
	       (unsigned long long)commit->at, (unsigned long long)at, refresh,
	       (unsigned long long)seq, flags);
	wp_presentation_feedback_destroy(feedback);
	free(commit);
}

static void feedback_discarded(void *data, struct wp_presentation_feedback *feedback)
{
	struct commit *commit = data;
	record("discarded %u %llu\n", commit->surface, (unsigned long long)commit->at);
	wp_presentation_feedback_destroy(feedback);
	free(commit);
}

static const struct wp_presentation_feedback_listener feedback_listener = {
	.sync_output = feedback_sync_output,
	.presented = feedback_presented,
	.discarded = feedback_discarded,
};

// Returns whether a buffer was attached to the surface since its last commit
// and forgets it. A full table records every commit.
static int take_attached(uint32_t surface)
{
	int found;

	pthread_mutex_lock(&surfaces_lock);
	found = attached_count == MAX_SURFACES;
	for (int i = 0; i < attached_count; i++) {
		if (attached[i] == surface) {
			attached[i] = attached[--attached_count];
			found = 1;
			break;
		}
	}
	pthread_mutex_unlock(&surfaces_lock);
	return found;
}

static void note_attached(uint32_t surface)
{
	pthread_mutex_lock(&surfaces_lock);
	for (int i = 0; i < attached_count; i++) {
		if (attached[i] == surface) {
			pthread_mutex_unlock(&surfaces_lock);
			return;
		}
	}
	if (attached_count < MAX_SURFACES)
		attached[attached_count++] = surface;
	pthread_mutex_unlock(&surfaces_lock);
}

// Called before the commit is forwarded, so the request precedes it on the wire.
static void request_feedback(struct wl_proxy *surface)
{
	struct commit *commit;
	struct wp_presentation_feedback *feedback;

	if (!presentation || wl_proxy_get_display(surface) != display ||
	    !take_attached(wl_proxy_get_id(surface)))
		return;
	// The client's reader thread queues our events; handle the ones that arrived.
	wl_display_dispatch_queue_pending(display, queue);
	commit = malloc(sizeof(*commit));
	if (!commit)
		return;
	commit->surface = wl_proxy_get_id(surface);
	commit->at = now();
	feedback = wp_presentation_feedback(presentation, (struct wl_surface *)surface);
	wp_presentation_feedback_add_listener(feedback, &feedback_listener, commit);
}

struct wl_proxy *wl_proxy_marshal_flags(struct wl_proxy *proxy, uint32_t opcode,
					const struct wl_interface *interface,
					uint32_t version, uint32_t flags, ...)
{
	union wl_argument args[MAX_ARGS];
	const struct wl_message *message;
	const char *signature;
	va_list ap;
	int count = 0;

	pthread_once(&resolved, resolve);
	message = &wl_proxy_get_interface(proxy)->methods[opcode];
	// Mirrors libwayland's wl_argument_from_va_list.
	va_start(ap, flags);
	for (signature = message->signature; *signature && count < MAX_ARGS; signature++) {
		switch (*signature) {
		case 'i':
			args[count++].i = va_arg(ap, int32_t);
			break;
		case 'u':
			args[count++].u = va_arg(ap, uint32_t);
			break;
		case 'f':
			args[count++].f = va_arg(ap, wl_fixed_t);
			break;
		case 's':
			args[count++].s = va_arg(ap, const char *);
			break;
		case 'o':
		case 'n':
			args[count++].o = va_arg(ap, struct wl_object *);
			break;
		case 'a':
			args[count++].a = va_arg(ap, struct wl_array *);
			break;
		case 'h':
			args[count++].h = va_arg(ap, int32_t);
			break;
		}
	}
	va_end(ap);
	if ((opcode == WL_SURFACE_ATTACH || opcode == WL_SURFACE_COMMIT) &&
	    strcmp(wl_proxy_get_class(proxy), "wl_surface") == 0) {
		if (opcode == WL_SURFACE_ATTACH)
			note_attached(wl_proxy_get_id(proxy));
		else
			request_feedback(proxy);
	}
	return real_marshal_array_flags(proxy, opcode, interface, version, flags, args);
}

static void presentation_clock_id(void *data, struct wp_presentation *wp_presentation,
				  uint32_t clk_id)
{
	clock_id = (clockid_t)clk_id;
	record("clock %u\n", clk_id);
}

static const struct wp_presentation_listener presentation_listener = {
	.clock_id = presentation_clock_id,
};

static void registry_global(void *data, struct wl_registry *registry, uint32_t name,
			    const char *interface, uint32_t version)
{
	if (presentation || strcmp(interface, wp_presentation_interface.name) != 0)
		return;
	presentation = wl_registry_bind(registry, name, &wp_presentation_interface, 1);
	wp_presentation_add_listener(presentation, &presentation_listener, NULL);
}

static void registry_global_remove(void *data, struct wl_registry *registry, uint32_t name)
{
}

static const struct wl_registry_listener registry_listener = {
	.global = registry_global,
	.global_remove = registry_global_remove,
};

// Binds wp_presentation on a private queue of the client's first display.
static void attach(struct wl_display *connected)
{
	const char *path = getenv("WHEREAMI_PRESENT_LOG");
	struct wl_display *wrapper;
	struct wl_registry *registry;

	if (!connected || display || !path)
		return;
	out = fopen(path, "w");
	if (!out) {
		perror("wayland-present: open log");
		return;
	}
	setvbuf(out, NULL, _IOLBF, 0);
	record("wayland-present 1\n");
	display = connected;
	queue = wl_display_create_queue(display);
	wrapper = wl_proxy_create_wrapper(display);
	wl_proxy_set_queue((struct wl_proxy *)wrapper, queue);
	registry = wl_display_get_registry(wrapper);
	wl_proxy_wrapper_destroy(wrapper);
	wl_registry_add_listener(registry, &registry_listener, NULL);
	// Globals, then the presentation clock.
	wl_display_roundtrip_queue(display, queue);
	if (presentation)
		wl_display_roundtrip_queue(display, queue);
	else
		record("unsupported\n");
}

struct wl_display *wl_display_connect(const char *name)
{
	struct wl_display *connected;

	pthread_once(&resolved, resolve);
	connected = real_connect(name);
	attach(connected);
	return connected;
}

struct wl_display *wl_display_connect_to_fd(int fd)
{
	struct wl_display *connected;

	pthread_once(&resolved, resolve);
	connected = real_connect_to_fd(fd);
	attach(connected);
	return connected;
}
