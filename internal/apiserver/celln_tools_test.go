package apiserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-logr/logr"
	api "github.com/sympozium-ai/sympozium/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCellnCatalogueListIsNamespaceScopedSortedAndReadOnly(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := api.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	object := func(ns, name string) *api.CellnTool {
		return &api.CellnTool{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}, Spec: api.CellnToolSpec{Revision: "v1"}}
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(object("default", "z"), object("default", "a"), object("other", "private")).Build()
	s := NewServer(c, nil, nil, logr.Discard())
	for _, tc := range []struct {
		query string
		want  []string
	}{{"", []string{"a", "z"}}, {"?namespace=other", []string{"private"}}, {"?namespace=empty", []string{}}} {
		res := httptest.NewRecorder()
		s.Handler(nil).ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v1/celln-tools"+tc.query, nil))
		if res.Code != 200 {
			t.Fatalf("list: %d", res.Code)
		}
		var tools []api.CellnTool
		if err := json.Unmarshal(res.Body.Bytes(), &tools); err != nil {
			t.Fatal(err)
		}
		if tools == nil || len(tools) != len(tc.want) {
			t.Fatal("wrong namespace or null empty list")
		}
		for i, name := range tc.want {
			if tools[i].Name != name {
				t.Fatal("unstable order or namespace leak")
			}
		}
	}
	res := httptest.NewRecorder()
	s.Handler(nil).ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/api/v1/celln-tools", nil))
	if res.Code != http.StatusMethodNotAllowed {
		t.Fatalf("catalogue mutation exposed: %d", res.Code)
	}
}
