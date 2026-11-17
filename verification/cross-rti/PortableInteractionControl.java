package org.gorti.rti;

import hla.rti1516e.*;
import java.net.URL;
import java.util.UUID;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;

import static org.junit.jupiter.api.Assertions.*;

/** A separate delivery control, not a replacement for the retained regression cases. */
class PortableInteractionControl {
    @Test
    @Timeout(90)
    void twoSessionsDeliverTheSameReceiveOrderPayload() throws Exception {
        RTIambassador source = TestRti.ambassador();
        RTIambassador sink = TestRti.ambassador();
        Receiver callbacks = new Receiver();
        String federation = "cross-rti-control-" + UUID.randomUUID();
        boolean sourceConnected = false, sinkConnected = false;
        boolean created = false, sourceJoined = false, sinkJoined = false;
        try {
            TestRti.connect(source, new NullFederateAmbassador());
            sourceConnected = true;
            TestRti.connect(sink, callbacks);
            sinkConnected = true;
            URL fom = getClass().getResource("/exchange-fom.xml");
            assertNotNull(fom);
            source.createFederationExecution(federation, new URL[] {fom});
            created = true;
            source.joinFederationExecution("source", "control", federation);
            sourceJoined = true;
            sink.joinFederationExecution("sink", "control", federation);
            sinkJoined = true;
            InteractionClassHandle sentClass = source.getInteractionClassHandle("HLAinteractionRoot.Pulse");
            InteractionClassHandle receivedClass = sink.getInteractionClassHandle("HLAinteractionRoot.Pulse");
            ParameterHandle receivedParameter = sink.getParameterHandle(receivedClass, "payload");
            source.publishInteractionClass(sentClass);
            sink.subscribeInteractionClass(receivedClass);
            source.enableAsynchronousDelivery();
            sink.enableAsynchronousDelivery();
            ParameterHandleValueMap values = source.getParameterHandleValueMapFactory().create(1);
            byte[] payload = {0, 0, 0, 42};
            byte[] tag = {1, 5, 1, 6};
            values.put(source.getParameterHandle(sentClass, "payload"), payload);
            source.sendInteraction(sentClass, values, tag);
            long deadline = System.nanoTime() + 10_000_000_000L;
            while (callbacks.delivery.get() == null && System.nanoTime() < deadline) {
                source.evokeMultipleCallbacks(0.001, 0.02);
                sink.evokeMultipleCallbacks(0.001, 0.02);
            }
            Delivery received = callbacks.delivery.get();
            assertNotNull(received, "interaction callback not received");
            assertEquals(receivedClass, received.type);
            assertEquals(1, received.values.size());
            assertArrayEquals(payload, received.values.get(receivedParameter));
            assertArrayEquals(tag, received.tag);
            assertEquals(OrderType.RECEIVE, received.order);
            // Observe a short additional window; do not claim unbounded exactly-once delivery.
            for (int i = 0; i < 5; i++) {
                source.evokeMultipleCallbacks(0.001, 0.02);
                sink.evokeMultipleCallbacks(0.001, 0.02);
            }
            assertEquals(1, callbacks.count.get());
        } finally {
            if (sinkJoined) sink.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST);
            if (sourceJoined) source.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST);
            if (created) source.destroyFederationExecution(federation);
            if (sinkConnected) sink.disconnect();
            if (sourceConnected) source.disconnect();
        }
    }

    private static final class Delivery {
        InteractionClassHandle type;
        ParameterHandleValueMap values;
        byte[] tag;
        OrderType order;
    }

    private static final class Receiver extends NullFederateAmbassador {
        final AtomicReference<Delivery> delivery = new AtomicReference<>();
        final AtomicInteger count = new AtomicInteger();

        @Override
        public void receiveInteraction(InteractionClassHandle type, ParameterHandleValueMap values,
                byte[] tag, OrderType order, TransportationTypeHandle transport, SupplementalReceiveInfo info) {
            Delivery current = new Delivery();
            current.type = type;
            current.values = values;
            current.tag = tag.clone();
            current.order = order;
            count.incrementAndGet();
            delivery.set(current);
        }
    }
}
