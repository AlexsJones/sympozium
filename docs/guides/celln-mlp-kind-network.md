# Isolated Kind pod-to-host proof: network boundary

This is a test-topology preparation note, not a completed controller-Pod proof
or production network recipe. Keep the explicit `kind-celln-deployed` context;
do not use the ambient/default cluster or bind services to the host LAN.

The development cluster is owned by rootless Podman. On 2026-09-08 its private
network namespace had the `kind` gateway `10.89.0.1`, with Kind nodes on the
same private bridge. The ordinary host namespace had no interface at that
address. Inspect the current provider/network before reusing these addresses.

A credential-free TCP diagnostic established these facts:

- A node request to `host.containers.internal` (`169.254.1.2` here) could not
  connect to a server bound only to the ordinary host's `127.0.0.1`.
- A listener bound to `10.89.0.1` under `podman unshare --rootless-netns` received
  the actual node's `/mlp-private-network-probe` request. It deliberately sent no
  HTTP response, so curl reported an empty reply; the listener received the
  request bytes. The diagnostic listener exited afterward.
- This establishes node-to-private-gateway TCP only. It does not prove Pod
  networking, TLS authentication, Kubernetes approval reads or KVM execution in
  that namespace. Those must pass together before deployment is claimed.

The prepared controller image is `localhost/sympozium-celln-controller:e2edef1`,
image ID `2b02f54ba75244ea9eb03abd514f76fbfcc6f5b29836d2ea7ed61a570200816e`.
It was built from the committed source archive and loaded into the isolated
cluster with the Podman image-archive workflow in the MLP installation guide.
Loading an image is not a controller rollout or an execution proof.

The planned private-gateway proof must retain verified TLS on the controller's
issuer/router connections. Certificates must name their actual reachable IP or
DNS endpoint and use fresh private keys, not Go's publicly known httptest key.
The integration helper now generates independent, short-lived certificates and
independent issuer/router/backend tokens for each run. Never solve a name or
connectivity error with skip-verification, public fixture execution credentials,
or an unreviewed host-LAN listener.

Moving a host service into the private network namespace also changes its
Kubernetes API route. Its explicit kubeconfig must still verify the API server
certificate and use the intended Kind cluster. Do not assume the outer-host
loopback API URL remains reachable, or modify the user's original kubeconfig.
The standalone issuer must retain only the required approval-read authority;
provider credentials and the admitted store stay out of controller/API Pods.
