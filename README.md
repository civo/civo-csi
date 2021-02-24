# Civo CSI Driver

This controller is installed in to Civo K3s client clusters and handles the mounting of Civo Volumes on to the
correct nodes and promoting the storage into the cluster as a Persistent Volume.

## Background reading

* [Official Kubernetes CSI announcement blog](https://kubernetes.io/blog/2019/01/15/container-storage-interface-ga/)
* [Official CSI documentation](https://kubernetes-csi.github.io/docs/)
* [Good list of current CSI drivers to see how others have done things](https://kubernetes-csi.github.io/docs/drivers.html)
* [Presentation on how CSI is architected](https://www.usenix.org/sites/default/files/conference/protected-files/vault20_slides_seidman.pdf)
* [Example Hostpath CSI driver](https://github.com/kubernetes-csi/csi-driver-host-path/)
* [Notes on Hostpath CSI driver](https://www.velotio.com/engineering-blog/kubernetes-csi-in-action-explained-with-features-and-use-cases)

## Key takeaways

* We need to enable [dynamic provisioning](https://kubernetes.io/blog/2019/01/15/container-storage-interface-ga/#dynamic-provisioning)
* We're going to build a single binary and use the sidecars to register the appropriate parts of it in the appropriate place (one part runs on the control plane as a deployment, the other part runs on each node as a DaemonSet)

## Known issues

No currently known issues.

## Getting started

Normally for our Civo Kubernetes integrations we'd recommend visiting the [getting started document for CivoStack](https://github.com/civo/civo-stack/blob/master/GETTING_STARTED.md) guide, but this is a different situation (installed on the client cluster, not the supercluster), so below are some similar sort of steps to get you started:

### How do I run the driver in development

Unlike Operators, you can't as easily run CSI drivers locally just connected in to a cluster (there is a way with `socat` and forwarding Unix sockets, but we haven't experimented with that).

So the way we test our work is:

#### A. Run the CSI Sanity test suite

This is already integrated and is a simple `go test` away 🥳

This will run the full Kubernetes Storage SIG's suiet of tests against the endpoints you're supposed to have implemented.

#### B. Install in to a cluster

The steps are:

1. Create an environment variable called `IMAGE_NAME` with a random or recognisable name (`IMAGE_NAME=$(uuidgen)` works well)
2. Build the Docker image with `docker build -t ttl.sh/${IMAGE_NAME}:2h .`
3. Push the Docker image to ttl.sh (a short lived Docker image repository, useful for dev): `docker push ttl.sh/${IMAGE_NAME}:2h`
4. Copy recursively the `deploy/kubernetes` folder to `deploy/kubernetes-dev` and replace all occurences of `civo-csi:latest` in there with `YOUR_IMAGE_NAME:2h` (ENV variable interpolation won't work here), this folder is automatically in `.gitignore`
5. In a test cluster (a Civo K3s 1 node cluster will work) you'll need to create a `Secret` within the `civo-system` called `api-access` containing the keys `api_key` and `api_url` set to your Civo API key and either `https://api.civo.com` or a xip.io/ngrok pointing to your local development environment (depending on where your cluster is running)
6. Deploy the Kubernetes resources required to the cluster with `kubectl apply -f deploy/kubernetes-dev`
