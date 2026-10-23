# Demo AI Shootout — Multi-Proxy Benchmark

Automates bring-up, benchmarking, and teardown for the 3-way AI inference proxy shootout:
- **F5 BNK 2.4** (F5 BNK AI Service Plane via Gateway API on VIP `.130`)
- **HAProxy Ingress**
- **Envoy**

All three legs front the same backend inference engine to deliver an apples-to-apples performance comparison of TTFT, ITL, token throughput, and prefix caching hit rates.

---

## Usage

```bash
# Bring up shootout pods and services
bash examples/demo-ai/shootout/bringup.sh

# Run prefix cache benchmark
bash examples/demo-ai/shootout/prefix-cache.sh

# Clean up shootout resources
bash examples/demo-ai/shootout/teardown.sh
```
