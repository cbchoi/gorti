package object

import (
	"context"
	"math"
	"slices"
	"time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type reflectionRateKey struct {
	subscriber core.FederateHandle
	attribute  core.AttributeHandle
}

type reflectionDeliveryTime struct{ at time.Time }

type objectSubscriptionOptionsReader interface {
	ObjectSubscriptionOptions(core.FederationName, core.FederateHandle, core.ObjectClassHandle, core.AttributeHandle) (core.SubscriptionOptions, bool)
}

type regionalSubscriptionOptionsReader interface {
	RegionalAttributeSubscriptionOptions(core.FederationName, core.FederateHandle, core.ObjectHandle, core.AttributeHandle) []core.SubscriptionOptions
}

// Called with the object's delivery lock held, after current subscription
// projection and time-gate classification. Buffered delivery is not throttled
// here: its eventual wall-clock delivery has not happened yet.
func (p *preparedAttributeFanoutOf[T]) reduceImmediateBestEffort(ctx context.Context) {
	reader, ok := p.registry.opts.Declarations.(objectSubscriptionOptionsReader)
	if !ok {
		return
	}
	kept := p.immediate[:0]
	for _, delivery := range p.immediate {
		event, ok := delivery.Event.(*CallbackEventOf[T])
		if !ok || event.Kind != CallbackReflect || event.Metadata == nil || event.Metadata.Transportation != core.TransportTypeBestEffort {
			kept = append(kept, delivery)
			continue
		}
		drop := p.reflectionRateLimited(ctx, reader, delivery.Recipient, event)
		if drop {
			// Dropping the whole passel preserves 6.1.11; rate reduction must
			// not split otherwise identically transported attribute values.
			if i := slices.Index(p.reflectSubs, delivery.Recipient); i >= 0 {
				p.reflectSubs = slices.Delete(p.reflectSubs, i, i+1)
			}
			continue
		}
		kept = append(kept, delivery)
	}
	p.immediate = kept
}

func (p *preparedAttributeFanoutOf[T]) recordReflectionDelivery(ctx context.Context, delivery core.OutboxDelivery) {
	event, ok := delivery.Event.(*CallbackEventOf[T])
	if !ok || event.Kind != CallbackReflect {
		return
	}
	if p.instance.lastReflection == nil {
		p.instance.lastReflection = make(map[reflectionRateKey]reflectionDeliveryTime)
	}
	now := p.registry.opts.Clock.Now()
	for attr := range event.Attributes {
		if actual, found := p.registry.attributeAtClass(ctx, p.fed, event.ObjectClass, p.instance.cls, core.AttributeHandle(attr)); found {
			p.instance.lastReflection[reflectionRateKey{delivery.Recipient, actual}] = reflectionDeliveryTime{now}
		}
	}
}

func (p *preparedAttributeFanoutOf[T]) reflectionRateLimited(ctx context.Context, reader objectSubscriptionOptionsReader, recipient core.FederateHandle, event *CallbackEventOf[T]) bool {
	drop := false
	for attr := range event.Attributes {
		actual, found := p.registry.attributeAtClass(ctx, p.fed, event.ObjectClass, p.instance.cls, core.AttributeHandle(attr))
		if !found {
			continue
		}
		var options []core.SubscriptionOptions
		var regions []DDMRegionHandle
		if p.registry.opts.DDM != nil {
			regions = p.registry.opts.DDM.PublisherRegionsFor(p.fed, p.instance.handle, actual)
		}
		if p.registry.publisherRegionsOverlapDefault(p.fed, p.instance.cls, actual, regions) {
			if ordinary, subscribed := reader.ObjectSubscriptionOptions(p.fed, recipient, event.ObjectClass, core.AttributeHandle(attr)); subscribed {
				options = append(options, ordinary)
			}
		}
		if regional, ok := p.registry.opts.Declarations.(regionalSubscriptionOptionsReader); ok {
			options = append(options, regional.RegionalAttributeSubscriptionOptions(p.fed, recipient, p.instance.handle, actual)...)
		}
		limited, rate := p.subscriptionRateLimit(ctx, options)
		if !limited {
			continue
		}
		last, delivered := p.instance.lastReflection[reflectionRateKey{recipient, actual}]
		interval := math.Ceil(float64(time.Second) / rate)
		if delivered && (interval >= float64(math.MaxInt64) || p.registry.opts.Clock.Now().Sub(last.at) < time.Duration(interval)) {
			drop = true
			break
		}
	}
	return drop
}

func (p *preparedAttributeFanoutOf[T]) subscriptionRateLimit(ctx context.Context, options []core.SubscriptionOptions) (bool, float64) {
	limited, rate := false, float64(0)
	for _, option := range options {
		if option.UpdateRateDesignator == "" {
			limited = false
			break
		}
		model, err := p.registry.subscriptionModel(ctx, p.fed)
		if err != nil {
			continue
		}
		if rates, ok := model.(core.FOMUpdateRates); ok {
			if value, exists := rates.UpdateRateValue(option.UpdateRateDesignator); exists && value > 0 && !math.IsInf(value, 0) && !math.IsNaN(value) {
				limited = true
				rate = math.Max(rate, value)
			}
		}
	}
	return limited, rate
}
