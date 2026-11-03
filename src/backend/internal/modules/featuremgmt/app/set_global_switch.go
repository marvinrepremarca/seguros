package app

import (
	"context"
	"strconv"

	"github.com/marvinrepremarca/seguros/internal/modules/featuremgmt/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// SetGlobalSwitchCommand fija (Enabled != nil) o elimina un switch global (solo plataforma).
type SetGlobalSwitchCommand struct {
	Key     flags.Key
	Enabled *bool
	Reason  string
}

// SetGlobalSwitchHandler cambia switches globales (solo plataforma).
type SetGlobalSwitchHandler struct{ Deps }

// NewSetGlobalSwitchHandler construye el handler.
func NewSetGlobalSwitchHandler(d Deps) *SetGlobalSwitchHandler { return &SetGlobalSwitchHandler{d} }

// Handle cambia un kill switch global. Solo lo invoca cmd/platformctl. Sin tenant no hay
// fila de audit.events; el cambio queda en el log estructurado de la plataforma.
func (s *SetGlobalSwitchHandler) Handle(ctx context.Context, cmd SetGlobalSwitchCommand) error {
	if _, ok := s.reg.Get(cmd.Key); !ok {
		return domain.ErrUnknownFlag
	}
	reason, err := domain.NewReason(cmd.Reason)
	if err != nil {
		return err
	}
	if err := s.store.SetGlobalSwitch(ctx, cmd.Key, cmd.Enabled, string(reason), s.clock.Now()); err != nil {
		return err
	}
	value := "cleared"
	if cmd.Enabled != nil {
		value = strconv.FormatBool(*cmd.Enabled)
	}
	s.logger.InfoContext(ctx, "switch global de flag cambiado", "flag", string(cmd.Key), "value", value, "reason", string(reason))
	s.invalidate(ctx, kernel.TenantID{})
	return nil
}
