package gcjwt_test

import "github.com/go-kratos/kratos/v2/transport"

type mockHeader struct {
	m map[string][]string
}

func newMockHeader() *mockHeader { return &mockHeader{m: make(map[string][]string)} }

func (m *mockHeader) Get(key string) string {
	vals := m.m[key]
	if len(vals) > 0 {
		return vals[0]
	}
	return ""
}

func (m *mockHeader) Set(key, value string) { m.m[key] = []string{value} }

func (m *mockHeader) Add(key, value string) { m.m[key] = append(m.m[key], value) }

func (m *mockHeader) Keys() []string {
	keys := make([]string, 0, len(m.m))
	for k := range m.m {
		keys = append(keys, k)
	}
	return keys
}

func (m *mockHeader) Values(key string) []string { return m.m[key] }

// helper transports share header mocks.
type mockClientTransport struct {
	header *mockHeader
}

func (m *mockClientTransport) Kind() transport.Kind            { return transport.KindGRPC }
func (m *mockClientTransport) Endpoint() string                { return "test" }
func (m *mockClientTransport) Operation() string               { return "op" }
func (m *mockClientTransport) RequestHeader() transport.Header { return m.header }
func (m *mockClientTransport) ReplyHeader() transport.Header   { return newMockHeader() }

type mockServerTransport struct {
	header *mockHeader
}

func (m *mockServerTransport) Kind() transport.Kind            { return transport.KindGRPC }
func (m *mockServerTransport) Endpoint() string                { return "test" }
func (m *mockServerTransport) Operation() string               { return "op" }
func (m *mockServerTransport) RequestHeader() transport.Header { return m.header }
func (m *mockServerTransport) ReplyHeader() transport.Header   { return newMockHeader() }
