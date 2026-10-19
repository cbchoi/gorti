package object

import "github.com/cbchoi/gorti/rti/internal/hla/core"

func (p *preparedAttributeFanoutOf[T]) recordSentTransport() {
	if hook := p.registry.opts.OnObjectSentTransport; hook != nil {
		for i, transport := range []core.TransportType{core.TransportTypeReliable, core.TransportTypeBestEffort} {
			if p.sentTransports[i] {
				hook(p.fed, p.producer, p.class, transport)
			}
		}
	}
}
