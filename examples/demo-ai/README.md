# demo-ai — F5 BNK AI Service Plane & Inference Benchmarking

| Attribute | Specification |
|---|---|
| **Cluster Name** | `bnk-demo-ai` |
| **Location** | `eastus2` |
| **Pattern** | `dual-interface` with Accelerated Networking |
| **Simulated Model** | `llama-3.1-8b` (`NousResearch/Meta-Llama-3.1-8B-Instruct`) |
| **Model Serving** | Synthetic vLLM simulator (`llm-d-inference-sim`) or AKS GPU node |
| **Estimated Cost** | ~$3.50 - $6.00 / hour |

This blueprint provisions an AKS cluster optimized for evaluating the **F5 BNK AI Service Plane** (see the [F5 BIG-IP Next for Kubernetes CloudDocs](https://clouddocs.f5.com/bigip-next-for-kubernetes/latest/overview.html)). It exercises:
1. **Streaming Token Throughput & Latency**: Measures Time-to-First-Token (TTFT) and Inter-Token Latency (ITL).
2. **Token Quota Counting & Rate Limiting**: Token-bucket throttling per consumer/tenant.
3. **Semantic Prompt Caching**: Reducing LLM invocation costs and TTFT for semantically similar queries.
4. **Multi-Proxy Shootout**: Direct head-to-head comparison between F5 BNK, HAProxy, and Envoy.

---

## Quick Start

### 1. Validate & Provision
```bash
./azrbnkctl validate examples/demo-ai/cluster.yaml
./azrbnkctl up -f examples/demo-ai/cluster.yaml
export KUBECONFIG=.azrbnkctl/bnk-demo-ai/kubeconfig
```

### 2. Run the End-to-End AI Inference Scenario
```bash
./azrbnkctl scenarios run ai-inference-e2e -f examples/demo-ai/cluster.yaml
```

### 3. Run the Multi-Proxy Shootout
```bash
bash examples/demo-ai/shootout/bringup.sh
bash examples/demo-ai/shootout/prefix-cache.sh
```

### 4. Teardown
```bash
bash examples/demo-ai/shootout/teardown.sh
./azrbnkctl down -f examples/demo-ai/cluster.yaml
```
