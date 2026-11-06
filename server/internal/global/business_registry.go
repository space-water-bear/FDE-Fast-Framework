package global

import "sync"

type BusinessRegistration struct{ ObjectKey, TableName, Path, Name, Component, SchemaJSON string }

var generatedBusinessRegistry = struct {
	sync.RWMutex
	values map[string]BusinessRegistration
}{values: map[string]BusinessRegistration{}}

func RegisterGeneratedBusiness(registration BusinessRegistration) {
	generatedBusinessRegistry.Lock()
	defer generatedBusinessRegistry.Unlock()
	if _, exists := generatedBusinessRegistry.values[registration.ObjectKey]; exists {
		panic("duplicate managed business object: " + registration.ObjectKey)
	}
	generatedBusinessRegistry.values[registration.ObjectKey] = registration
}
func GeneratedBusiness(key string) (BusinessRegistration, bool) {
	generatedBusinessRegistry.RLock()
	defer generatedBusinessRegistry.RUnlock()
	value, ok := generatedBusinessRegistry.values[key]
	return value, ok
}

func GeneratedBusinessSchemaJSON(key string) string {
	registration, ok := GeneratedBusiness(key)
	if !ok {
		return ""
	}
	return registration.SchemaJSON
}
