# go-earcut

This directory vendors `github.com/rclancey/go-earcut` at commit
`f3ec78d874709ec6e9f85da3cd0e7e641d73d897`.

The implementation is a Go port by Ryan Clancey of Mapbox's Earcut algorithm.
The source carries two local correctness guards: z-order coordinates are
quantized after scaling, and empty hole lists are skipped. See `LICENSE` for
the upstream ISC license.
