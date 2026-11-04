package app

import (
	"context"
	"strconv"
	"strings"

	"github.com/marvinrepremarca/seguros/internal/modules/featuremgmt/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// SetGlobalSwitchCommand fija (Enabled != nil) o elimina un switch global (solo plataforma).
type SetGlobalSwitchCommand struct {
	Key     flags.Key
	Enabled *bool
	Reason  string
	// Operator identifica a la persona que ejecuta platformctl (obligatorio; queda en el historial).
	Operator string
	// ConfirmControl repite la clave para apagar un flag de control (ADR 0012).
	ConfirmControl string
}

// SetGlobalSwitchHandler cambia switches globales (solo plataforma).
type SetGlobalSwitchHandler struct{ Deps }

// NewSetGlobalSwitchHandler construye el handler.
func NewSetGlobalSwitchHandler(d Deps) *SetGlobalSwitchHandler { return &SetGlobalSwitchHandler{d} }

// Handle cambia un kill switch global. Solo lo invoca cmd/platformctl (rol seguros_platform); el
// cambio queda en featuremgmt.global_switch_history con operador, valor anterior y nuevo, y motivo.
func (s *SetGlobalSwitchHandler) Handle(ctx context.Context, cmd SetGlobalSwitchCommand) error {
	def, ok := s.reg.Get(cmd.Key)
	if !ok {
		return domain.ErrUnknownFlag
	}
	if def.Control && cmd.Enabled != nil && !*cmd.Enabled && cmd.ConfirmControl != string(cmd.Key) {
		return domain.ErrControlConfirmationRequired
	}
	reason, err := domain.NewReason(cmd.Reason)
	if err != nil {
		return err
	}
	operator := strings.TrimSpace(cmd.Operator)
	if operator == "" || len(operator) > 120 {
		return domain.ErrInvalidOperator
	}
	if err := s.store.SetGlobalSwitch(ctx, cmd.Key, cmd.Enabled, string(reason), operator, s.clock.Now()); err != nil {
		return err
	}
	value := "cleared"
	if cmd.Enabled != nil {
		value = strconv.FormatBool(*cmd.Enabled)
	}
	s.logger.InfoContext(ctx, "switch global de flag cambiado", "flag", string(cmd.Key), "value", value, "operator", operator)
	s.invalidate(ctx, kernel.TenantID{})
	return nil
}
