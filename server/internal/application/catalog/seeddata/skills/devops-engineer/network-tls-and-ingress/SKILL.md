---
name: network-tls-and-ingress
category: infrastructure
description: Use when a change involves DNS, TLS certificates, reverse proxies, ingress, ports, firewalls or private networking - and when debugging "it works locally but not through the proxy"
---

# Networking, TLS and Ingress

## Overview

**Core principle:** Traffic reaches an app through a chain — DNS → proxy/ingress → service → container port → the process's bind address. When something is unreachable, walk the chain in that order instead of guessing; each hop has a command that answers it.

## The chain, and how to check each hop

| Hop | Check | Common failure |
|---|---|---|
| DNS | `dig +short app.example.com` | Record points at the old server, or only AAAA exists |
| TLS | `curl -vI https://app.example.com` / `openssl s_client -connect host:443 -servername host` | Certificate issued for the apex only, expired, or issuance blocked because DNS was not ready |
| Proxy/ingress | Proxy logs; `kubectl describe ingress` | No route for the host, wrong service name/port, missing ingress class |
| Service | `kubectl get endpointslice -l kubernetes.io/service-name=<svc>` (the v1 `endpoints` API is deprecated since k8s 1.33) | No endpoints: the selector does not match any pod |
| Container port | `docker ps`, the exposed port | The published port differs from the app's port |
| Bind address | `ss -ltnp` inside the container | The app listens on `127.0.0.1`, so nothing outside the container can reach it |

## TLS

- Automated issuance (Let's Encrypt via cert-manager, Traefik, Caddy or the platform) is the default; a manual certificate is a renewal nobody will remember.
- HTTP-01 challenges need port 80 reachable and DNS already pointing at the server. DNS-01 is what you use for wildcards and for hosts not publicly reachable.
- Terminate TLS at the edge and redirect HTTP→HTTPS. `Strict-Transport-Security` applies to the host that sends it; add `includeSubDomains` only once every subdomain can serve HTTPS, and treat `preload` as effectively irreversible.
- Behind a proxy, the app must trust the forwarded headers (`X-Forwarded-Proto`/`For`) or it will generate `http://` links and log the proxy's IP as every client.
- Monitor expiry. An expired certificate is a full outage with a trivial cause.

## Exposure and firewalls

- Public: 80 and 443. Nothing else, unless the task says otherwise and says why.
- Databases, caches and queues bind to a private network or a container network — never a public interface, even "just for a moment", even with a password.
- SSH: key auth, no password auth, and restricted to known addresses where possible.
- Between services, prefer private networking (VPC, cluster network, docker network) over a public hostname plus an allowlist.
- In Kubernetes, default-deny NetworkPolicies and open the paths that are actually needed.

## Timeouts and sizes

Proxy defaults break real workloads quietly: upload limits (413), read timeouts on long requests (504), missing WebSocket upgrade headers, and buffering that defeats server-sent events. When a feature involves large uploads, long polling or streaming, set the matching proxy values in the same change — and state the values on the card.

## Red flags

- A DNS TTL of 24h on a record you are about to move (lower it beforehand).
- A certificate covering `example.com` but not `www.example.com`, or a wildcard used for a host one level deeper.
- An app reachable only by IP because the proxy route was never added — it works in the test and breaks for every user.
- A port opened on the host firewall to "debug", left open.
