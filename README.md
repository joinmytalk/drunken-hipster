drunken-hipster
===============

drunken-hipster is a WebSocket-aware SSL-capable HTTP reverse proxy/load
balancer.

Building
--------

First, make sure you have the Go build environment correctly installed. See
http://golang.org/ for more information.

Then run "make". This will in turn call the go utility to build the load
balancer, resulting in a binary named hipsterd.


Configuration
-------------

The principle of drunken-hipster is simple: you define one or more frontends
(i.e. the interface and port on which it shall listen on), and connect each
frontend with one or more backends (i.e. the IP address or host and port it
shall connect to). Requests that are received by the frontend are then
forwarded to each of the backends in a round-robin fashion, and the responses
will be sent back to the client.

Alternatively, you can define a frontend that handles a number of hostnames, as
indicated by the HTTP `Host` request header. For each hostname, you can again
define one or more backends. If a request is encountered with a `Host` header that
doesn't match any of the configured hostnames, the list of backends directly
associated with the frontend is used instead.

Confused? Let's take a look at a simple configuration example:

	[frontend frontend1]
	bind = 0.0.0.0:9000
	backends = backend1

	[backend backend1]
	connect = 127.0.0.1:8000

This is probably the simplest example possible. It defines a frontend that
binds to `0.0.0.0:9000`, and forwards all its incoming requests to only one
backend. This backend will send these forwarded requests to `127.0.0.1:9000`.

Now let's show a more complicated, host-based configuration:

	[frontend frontend1]
	bind = 0.0.0.0:9000
	hosts = example.com www.example.com foobar.com www.foobar.com
	backends = fallback

	[host example.com www.example.com]
	backends = backend1

	[host foobar.com www.foobar.com]
	backends = backend2

	[backend backend1]
	connect = 127.0.0.1:8000

	[backend backend2]
	connect = 192.168.0.1:8000

	[backend fallback]
	connect = 192.168.0.2:7000

The frontend binds to `0.0.0.0:9000`, and handles rquests with the `Host` headers
example.com, www.example.com, foobar.com and www.foobar.com. All other requests
are handled by the backend `fallback` and forwarded to `192.168.0.2:7000`.
Requests for example.com and www.example.com are handled by `backend1`, which
in turns forwards the requests to `127.0.0.1:8000`, while requests for foobar.com
and www.foobar.com are forwarded by "backend2" to `192.168.0.1:8000`.

If you want to have the original client's IP address forwarded to your backend
servers, you can optionally enable `X-Forwarded-For` headers:

	[frontend foo]
	bind = 0.0.0.0:8000
	backends = bar baz quux
	add-x-forwarded-for = true

drunken-hipster also supports WebSockets out of the box. No special
configuration is necessary. WebSockets are recognized by the `Connection:
upgrade` and `Upgrade: websocket` HTTP requests headers.

License
-------

See the file LICENSE for license information.


Author
------

Andreas Krennmair <ak@synflood.at>
