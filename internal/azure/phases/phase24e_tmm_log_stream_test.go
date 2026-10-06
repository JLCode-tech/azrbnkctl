package phases

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	k8swait "github.com/JLCode-tech/azrbnkctl/internal/k8s"
)

const phase24dShippedConf = "\n# To enable printing logs to stdout, uncomment the <store> block below:\n#\n# <store>\n#   @type stdout\n# </store>\n"

func phase24dObjects() []runtime.Object {
	return []runtime.Object{
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: k8swait.TMMLogCustomConfigMap, Namespace: k8swait.TMMLogNamespace}, Data: map[string]string{k8swait.TMMLogStdoutKey: phase24dShippedConf}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "f5-toda-fluentd-old", Namespace: k8swait.TMMLogNamespace, Labels: map[string]string{"app": "toda-fluentd"}}},
	}
}

func TestPhase24d_EnablesStoreAndBouncesFluentd(t *testing.T) {
	st, _ := state.Load(t.TempDir())
	cs := k8sfake.NewClientset(phase24dObjects()...)
	old := phase24dWait
	phase24dWait = 0 // no replacement pod in the fake; skip the wait
	defer func() { phase24dWait = old }()

	if err := Phase24dTMMLogStream(context.Background(), dssmHostDeviceCluster(), st, &Clients{K8s: cs, Profile: "test"}, false); err != nil {
		t.Fatalf("Phase24dTMMLogStream: %v", err)
	}
	cm, _ := cs.CoreV1().ConfigMaps(k8swait.TMMLogNamespace).Get(context.Background(), k8swait.TMMLogCustomConfigMap, metav1.GetOptions{})
	if cm.Data[k8swait.TMMLogStdoutKey] != k8swait.TMMLogStdoutStore {
		t.Errorf("stdout.conf = %q, want the enabled store", cm.Data[k8swait.TMMLogStdoutKey])
	}
	pods, _ := cs.CoreV1().Pods(k8swait.TMMLogNamespace).List(context.Background(), metav1.ListOptions{})
	if len(pods.Items) != 0 {
		t.Errorf("fluentd pod not bounced: %d left", len(pods.Items))
	}
	if st.Get("TMM_LOG_STREAM_ENABLED_AT") == "" || st.Get("TMM_LOG_STREAM_ENABLED_AT") == "dry-run" {
		t.Errorf("TMM_LOG_STREAM_ENABLED_AT = %q", st.Get("TMM_LOG_STREAM_ENABLED_AT"))
	}
	// Second run: store already on, nothing mutated.
	before := len(cs.Actions())
	if err := Phase24dTMMLogStream(context.Background(), dssmHostDeviceCluster(), st, &Clients{K8s: cs, Profile: "test"}, false); err != nil {
		t.Fatalf("second run: %v", err)
	}
	for _, a := range cs.Actions()[before:] {
		if v := a.GetVerb(); v == "update" || v == "delete" {
			t.Errorf("second run mutated the cluster: %s", v)
		}
	}
}

func TestPhase24d_DryRun_NoMutation(t *testing.T) {
	st, _ := state.Load(t.TempDir())
	cs := k8sfake.NewClientset(phase24dObjects()...)
	if err := Phase24dTMMLogStream(context.Background(), dssmHostDeviceCluster(), st, &Clients{K8s: cs, Profile: "test"}, true); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	for _, a := range cs.Actions() {
		if v := a.GetVerb(); v == "update" || v == "delete" {
			t.Errorf("dry-run mutated the cluster: %s", v)
		}
	}
	if st.Get("TMM_LOG_STREAM_ENABLED_AT") != "dry-run" {
		t.Errorf("TMM_LOG_STREAM_ENABLED_AT = %q, want dry-run", st.Get("TMM_LOG_STREAM_ENABLED_AT"))
	}
}
