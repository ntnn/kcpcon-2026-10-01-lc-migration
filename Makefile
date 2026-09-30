GO ?= go
MINDL = $(GO) tool codeberg.org/ntnn/mindl
TOOLS_DIR = hack/tools

KUBECTL ?= kubectl
KIND ?= kind
HELM ?= helm

KCP_PLUGINS_VER := 0.33.1
KUBECTL_KCP := $(TOOLS_DIR)/kubectl-kcp-$(KCP_PLUGINS_VER)
KUBECTL_WS := $(TOOLS_DIR)/kubectl-ws-$(KCP_PLUGINS_VER)
KUBECTL_CREATE_WORKSPACE := $(TOOLS_DIR)/kubectl-create-workspace-$(KCP_PLUGINS_VER)

STALK_VER := 0.8.0-beta.2
STALK := $(TOOLS_DIR)/stalk-$(STALK_VER)

CERT_MANAGER_VER := v1.21.2

KIND_CLUSTER := kcp-lc-demo
KUBE_DIR := .kube
KIND_KUBECONFIG := $(KUBE_DIR)/kind.kubeconfig
KCP_KUBECONFIG := $(KUBE_DIR)/kcp.kubeconfig
KCP_NAMESPACE := kcp

# External hostname of the front-proxy, set in demo/kcp/rootshard.yaml.
KCP_HOSTNAME := front-proxy-front-proxy.$(KCP_NAMESPACE).svc.cluster.local
# Host port mapped to the front-proxy NodePort in demo/kind.yaml.
KCP_HOST_PORT := 6443

# etcd release -> NodePort, must match demo/kind.yaml.
ETCD_NODEPORT_root := 30379
ETCD_NODEPORT_shard-0 := 30380
ETCD_NODEPORT_shard-1 := 30381
ETCD_SHARDS := root shard-0 shard-1

K := $(KUBECTL) --kubeconfig $(KIND_KUBECONFIG)

.PHONY: tools
tools: $(KUBECTL_KCP) $(KUBECTL_WS) $(KUBECTL_CREATE_WORKSPACE) $(STALK)

$(KUBECTL_KCP):
	mkdir -p $(TOOLS_DIR)
	$(MINDL) download -common -out $@ -version $(KCP_PLUGINS_VER) \
		-url 'https://github.com/kcp-dev/kcp/releases/download/v{{.Version}}/kubectl-kcp-plugin_{{.Version}}_{{.OS}}_{{.Arch}}.tar.gz' \
		-inarchive 'bin/kubectl-kcp'
	ln -sf $(notdir $@) $(TOOLS_DIR)/kubectl-kcp

$(KUBECTL_WS):
	mkdir -p $(TOOLS_DIR)
	$(MINDL) download -common -out $@ -version $(KCP_PLUGINS_VER) \
		-url 'https://github.com/kcp-dev/kcp/releases/download/v{{.Version}}/kubectl-ws-plugin_{{.Version}}_{{.OS}}_{{.Arch}}.tar.gz' \
		-inarchive 'bin/kubectl-ws'
	ln -sf $(notdir $@) $(TOOLS_DIR)/kubectl-ws

$(KUBECTL_CREATE_WORKSPACE):
	mkdir -p $(TOOLS_DIR)
	$(MINDL) download -common -out $@ -version $(KCP_PLUGINS_VER) \
		-url 'https://github.com/kcp-dev/kcp/releases/download/v{{.Version}}/kubectl-create-workspace-plugin_{{.Version}}_{{.OS}}_{{.Arch}}.tar.gz' \
		-inarchive 'bin/kubectl-create-workspace'
	ln -sf $(notdir $@) $(TOOLS_DIR)/kubectl-create-workspace

$(STALK):
	mkdir -p $(TOOLS_DIR)
	$(MINDL) download -common -out $@ -version $(STALK_VER) \
		-url 'https://codeberg.org/xrstf/stalk/releases/download/v{{.Version}}/stalk_{{.Version}}_{{.OS}}_{{.Arch}}.{{.OSArchive}}' \
		-inarchive 'stalk_{{.Version}}_{{.OS}}_{{.Arch}}/stalk{{.Exe}}'
	ln -sf $(notdir $@) $(TOOLS_DIR)/stalk

.PHONY: up
up: tools cluster cert-manager etcd operator kcp kubeconfig

.PHONY: down
down:
	$(KIND) delete cluster --name $(KIND_CLUSTER)
	rm -rf $(KUBE_DIR)

.PHONY: cluster
cluster:
	mkdir -p $(KUBE_DIR)
	$(KIND) get clusters | grep -qx $(KIND_CLUSTER) \
		|| $(KIND) create cluster --name $(KIND_CLUSTER) --config demo/kind.yaml --kubeconfig $(KIND_KUBECONFIG)
	$(KIND) export kubeconfig --name $(KIND_CLUSTER) --kubeconfig $(KIND_KUBECONFIG)

.PHONY: cert-manager
cert-manager:
	$(HELM) upgrade --install --kubeconfig $(KIND_KUBECONFIG) \
		--namespace cert-manager --create-namespace \
		--version $(CERT_MANAGER_VER) \
		--set crds.enabled=true --wait \
		cert-manager oci://quay.io/jetstack/charts/cert-manager

.PHONY: etcd
etcd:
	$(foreach shard,$(ETCD_SHARDS),\
		$(HELM) upgrade --install --kubeconfig $(KIND_KUBECONFIG) \
			--namespace $(KCP_NAMESPACE) --create-namespace \
			--set nodePort=$(ETCD_NODEPORT_$(shard)) --wait \
			etcd-$(shard) demo/etcd || exit 1;)

.PHONY: operator
operator:
	$(K) apply --server-side -k demo/operator
	$(K) -n kcp-operator-system rollout status deployment/kcp-operator-controller-manager --timeout 5m

.PHONY: kcp
kcp:
	$(K) apply --server-side -k demo/kcp
	$(K) -n $(KCP_NAMESPACE) wait --for=jsonpath='{.status.phase}'=Running rootshard/root --timeout 10m
	$(K) -n $(KCP_NAMESPACE) wait --for=jsonpath='{.status.phase}'=Running shard/shard-0 shard/shard-1 --timeout 10m
	$(K) -n $(KCP_NAMESPACE) wait --for=jsonpath='{.status.phase}'=Running frontproxy/front-proxy --timeout 10m

# Rewrites the server to the kind host port, TLS still verifies against the external hostname.
.PHONY: kubeconfig
kubeconfig:
	$(K) -n $(KCP_NAMESPACE) wait --for=create secret/admin-kubeconfig --timeout 5m
	$(K) -n $(KCP_NAMESPACE) get secret admin-kubeconfig -o jsonpath='{.data.kubeconfig}' | base64 -d > $(KCP_KUBECONFIG)
	for cluster in $$($(KUBECTL) --kubeconfig $(KCP_KUBECONFIG) config view -o jsonpath='{.clusters[*].name}'); do \
		server=$$($(KUBECTL) --kubeconfig $(KCP_KUBECONFIG) config view -o jsonpath="{.clusters[?(@.name=='$$cluster')].cluster.server}"); \
		$(KUBECTL) --kubeconfig $(KCP_KUBECONFIG) config set-cluster $$cluster \
			--server $$(echo $$server | sed 's|https://$(KCP_HOSTNAME):6443|https://127.0.0.1:$(KCP_HOST_PORT)|') \
			--tls-server-name $(KCP_HOSTNAME) || exit 1; \
	done
	@echo "export KUBECONFIG=$(CURDIR)/$(KCP_KUBECONFIG) PATH=$(CURDIR)/$(TOOLS_DIR):\$$PATH"

ETCDVIEW := bin/etcdview
# Host ports mapped to the etcd NodePorts in demo/kind.yaml.
ETCDVIEW_ENDPOINTS := -etcd root=http://127.0.0.1:23790 -etcd shard-0=http://127.0.0.1:23791 -etcd shard-1=http://127.0.0.1:23792

.PHONY: $(ETCDVIEW)
$(ETCDVIEW):
	$(GO) build -o $@ ./tools/etcdview

# Usage: make etcdview CLUSTER=<logical cluster name>
.PHONY: etcdview
etcdview: $(ETCDVIEW)
	$(ETCDVIEW) $(ETCDVIEW_ENDPOINTS) -cluster $(CLUSTER)

DEMO := bin/demo

.PHONY: $(DEMO)
$(DEMO):
	$(GO) build -o $@ ./tools/demo

# DEMO_FLAGS selects the migrated workspace, e.g. DEMO_FLAGS="-parent root:demo -workspace tenant".
DEMO_FLAGS :=

# Steps through setup and migration, waiting for enter before each step.
.PHONY: demo
demo: $(DEMO) $(ETCDVIEW)
	$(DEMO) run -kubeconfig $(KCP_KUBECONFIG) $(DEMO_FLAGS)

.PHONY: seed
seed: $(DEMO)
	$(DEMO) seed -kubeconfig $(KCP_KUBECONFIG) $(DEMO_FLAGS)

.PHONY: commands
commands: $(DEMO) $(ETCDVIEW)
	$(DEMO) commands -kubeconfig $(KCP_KUBECONFIG) $(DEMO_FLAGS)

.PHONY: migrate
migrate: $(DEMO)
	$(DEMO) migrate -kubeconfig $(KCP_KUBECONFIG) $(DEMO_FLAGS)

# Deletes the demo workspace tree and its generated kubeconfigs.
.PHONY: clean-demo
clean-demo:
	$(KUBECTL) --kubeconfig $(KCP_KUBECONFIG) --server https://127.0.0.1:$(KCP_HOST_PORT)/clusters/root \
		delete workspace demo --ignore-not-found --wait --timeout 5m
	rm -f $(KUBE_DIR)/root-demo.kubeconfig $(KUBE_DIR)/root-demo-tenant.kubeconfig
