package control

import (
	"context"
	"io"

	"github.com/FPGSchiba/vcs-srs-client/internal/events"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

// ConsumeUpdates opens SubscribeToUpdates and routes each ServerUpdate into the
// store and the typed emitter. Returns when the stream ends or ctx is cancelled.
func (c *Client) ConsumeUpdates(ctx context.Context, st *state.Store, em events.Emitter) error {
	stream, err := c.rpc.SubscribeToUpdates(ctx, &srspb.Empty{}, c.callOpts()...)
	if err != nil {
		return err
	}
	tagged := events.New(em)
	for {
		upd, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		route(upd, st, tagged)
	}
}

func route(upd *srspb.ServerUpdate, st *state.Store, tagged *events.Tagged) {
	switch upd.GetType() {
	case srspb.ServerUpdate_CLIENT_JOINED, srspb.ServerUpdate_CLIENT_INFO_UPDATE:
		cu := upd.GetClientUpdate()
		if cu == nil || cu.GetClientInfo() == nil {
			return
		}
		guid := cu.GetClientGuid()
		st.UpdateClient(guid, cu.GetClientInfo())
		tagged.ClientUpdate(guid, events.ClientUpdatePayload{
			Name:      cu.GetClientInfo().GetName(),
			Coalition: cu.GetClientInfo().GetCoalition(),
			UnitId:    cu.GetClientInfo().GetUnitId(),
			RoleId:    cu.GetClientInfo().GetRoleId(),
		})
	case srspb.ServerUpdate_CLIENT_LEFT:
		cu := upd.GetClientUpdate()
		if cu == nil {
			return
		}
		st.RemoveClient(cu.GetClientGuid())
		tagged.ClientLeft(cu.GetClientGuid())
	case srspb.ServerUpdate_CLIENT_RADIO_UPDATE:
		cu := upd.GetClientUpdate()
		if cu == nil || cu.GetRadioInfo() == nil {
			return
		}
		st.SetRadios(cu.GetClientGuid(), cu.GetRadioInfo())
		tagged.RadioUpdate(cu.GetClientGuid(), radioInfoToPayload(cu.GetRadioInfo()))
	case srspb.ServerUpdate_SERVER_SETTINGS_CHANGED:
		if s := upd.GetSettingsUpdate(); s != nil {
			st.SetSettings(s)
			tagged.SettingsUpdate(struct{}{})
		}
	case srspb.ServerUpdate_SERVER_ACTION:
		tagged.ServerAction(struct{}{})
	default:
		// DISTRIBUTION_UPDATE / VOICE_ADDRESS_UPDATE / UNKNOWN — ignored this phase.
	}
}

func radioInfoToPayload(ri *srspb.RadioInfo) events.RadioInfoPayload {
	out := events.RadioInfoPayload{Muted: ri.GetMuted()}
	for _, r := range ri.GetRadios() {
		out.Radios = append(out.Radios, events.RadioPayload{
			ID:         r.GetId(),
			Name:       r.GetName(),
			Frequency:  r.GetFrequency(),
			Enabled:    r.GetEnabled(),
			IsIntercom: r.GetIsIntercom(),
		})
	}
	return out
}
