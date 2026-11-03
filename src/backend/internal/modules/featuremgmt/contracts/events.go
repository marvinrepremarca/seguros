// Package contracts es la única superficie pública de featuremgmt: sus eventos de integración.
package contracts

// TopicFeatureToggled es el tópico del evento FeatureToggled.
const TopicFeatureToggled = "featuremgmt.feature_toggled"

// FeatureToggled se publica (outbox) cada vez que cambia un override de flag de un tenant. El
// tenant viaja en los metadatos del evento. Sirve para invalidar cachés de forma confiable y para
// que los módulos reaccionen al encendido/apagado.
type FeatureToggled struct {
	FlagKey string `json:"flagKey"`
	// Enabled es el valor efectivo resultante para el tenant.
	Enabled bool `json:"enabled"`
}
