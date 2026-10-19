package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;

import com.google.protobuf.Message;
import com.google.protobuf.ByteString;
import hla.rti1516e.*;
import hla.rti1516e.exceptions.*;
import io.grpc.*;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.HashMap;
import java.util.Map;
import java.util.List;
import java.util.Set;
import org.gorti.rti.types.HandleCollections;
import org.gorti.rti.types.Handles;
import org.junit.jupiter.api.Test;
import rti.v1.Common;

class TransportationServicesTest {
    private final RecordingChannel wire = new RecordingChannel();
    private final Context context = new Context(wire);
    private final ExchangeServices exchange = new ExchangeServices(context);
    private final CapturingSink sink = new CapturingSink();
    private final TransportationServices service = new TransportationServices(context, sink, exchange);
    private final ObjectInstanceHandle object = Handles.objectInstance(31);
    private final AttributeHandle attribute = Handles.attribute(51);
    private final InteractionClassHandle interaction = Handles.interactionClass(41);
    private final TransportationTypeHandle reliable = Handles.transportation(1);
    private final TransportationTypeHandle bestEffort = Handles.transportation(2);

    TransportationServicesTest() { exchange.discovered(31, 21, "item"); }

    @Test void sixServicesUseExactRpcAndJoinedEnvelope() throws Exception {
        service.changeAttributeOrderType(object, attributes(51, 52), OrderType.RECEIVE);
        service.changeInteractionOrderType(interaction, OrderType.TIMESTAMP);
        service.requestAttributeTransportationTypeChange(object, attributes(51, 52), bestEffort);
        service.requestInteractionTransportationTypeChange(interaction, reliable);
        wire.response = response(2);
        service.queryAttributeTransportationType(object, attribute);
        service.queryInteractionTransportationType(Handles.federate(8), interaction);
        assertEquals(List.of("ChangeAttributeOrderType", "ChangeInteractionOrderType",
                "PrepareAttributeTransportationTypeChange", "PrepareInteractionTransportationTypeChange",
                "QueryAttributeTransportationType", "QueryInteractionTransportationType"), wire.methods);
        for (Message request : wire.requests) {
            assertEquals(Common.WireVersion.WIRE_VERSION_V1.getValueDescriptor(), field(request, "wire_version"));
            assertEquals("fed", field(request, "federation_name"));
            assertEquals(7L, field(request, "federate_handle"));
        }
        rti.v1.Object.ChangeAttributeOrderRequest attrOrder = (rti.v1.Object.ChangeAttributeOrderRequest) wire.requests.get(0);
        assertEquals(31, attrOrder.getObjectHandle());
        assertEquals(Set.of(51L, 52L), Set.copyOf(attrOrder.getAttributeHandlesList()));
        assertEquals(rti.v1.Object.OrderType.ORDER_TYPE_RECEIVE, attrOrder.getOrderType());
        rti.v1.Object.ChangeInteractionOrderRequest intOrder = (rti.v1.Object.ChangeInteractionOrderRequest) wire.requests.get(1);
        assertEquals(41, intOrder.getInteractionClassHandle());
        assertEquals(rti.v1.Object.OrderType.ORDER_TYPE_TIMESTAMP, intOrder.getOrderType());
        rti.v1.Object.QueryInteractionTransportationTypeRequest query = (rti.v1.Object.QueryInteractionTransportationTypeRequest) wire.requests.get(5);
        assertEquals(8, query.getTargetFederateHandle());
        assertEquals(41, query.getInteractionClassHandle());
        assertEquals(4, sink.queued.size());
        assertEquals(4, sink.captures);
    }

    @Test void allFourCallbacksAreDeferredAndCarryResponseSnapshotAndTargetProducer() throws Exception {
        AttributeHandleSet requested = attributes(51, 52);
        service.requestAttributeTransportationTypeChange(object, requested, bestEffort);
        requested.clear();
        service.requestInteractionTransportationTypeChange(interaction, reliable);
        wire.response = response(1);
        service.queryAttributeTransportationType(object, attribute);
        wire.response = response(2);
        service.queryInteractionTransportationType(Handles.federate(8), interaction);
        wire.response = response(1);
        List<String> delivered = new ArrayList<>();
        NullFederateAmbassador ambassador = new NullFederateAmbassador() {
            @Override public void confirmAttributeTransportationTypeChange(ObjectInstanceHandle target, AttributeHandleSet attrs, TransportationTypeHandle type) {
                assertEquals(object, target); assertEquals(attributes(51, 52), attrs); assertEquals(bestEffort, type);
                delivered.add("attributes"); attrs.clear();
            }
            @Override public void confirmInteractionTransportationTypeChange(InteractionClassHandle target, TransportationTypeHandle type) {
                assertEquals(interaction, target); assertEquals(reliable, type); delivered.add("interaction");
            }
            @Override public void reportAttributeTransportationType(ObjectInstanceHandle target, AttributeHandle attr, TransportationTypeHandle type) {
                assertEquals(object, target); assertEquals(attribute, attr); assertEquals(reliable, type); delivered.add("attribute-report");
            }
            @Override public void reportInteractionTransportationType(FederateHandle producer, InteractionClassHandle target, TransportationTypeHandle type) {
                assertEquals(Handles.federate(8), producer); assertEquals(interaction, target); assertEquals(bestEffort, type); delivered.add("interaction-report");
            }
        };
        assertTrue(delivered.isEmpty());
        for (CallbackSink.Invocation callback : sink.queued) callback.invoke(ambassador);
        assertEquals(List.of("attributes", "interaction", "attribute-report", "interaction-report"), delivered);
    }

    @Test void attributePendingIsAtomicPerObjectAndOverlapAndEndsAtConfirmation() throws Exception {
        service.requestAttributeTransportationTypeChange(object, attributes(51), bestEffort);
        assertThrows(AttributeAlreadyBeingChanged.class,
                () -> service.requestAttributeTransportationTypeChange(object, attributes(51, 52), reliable));
        service.requestAttributeTransportationTypeChange(object, attributes(52), reliable);
        exchange.discovered(32, 21, "other");
        service.requestAttributeTransportationTypeChange(Handles.objectInstance(32), attributes(51), reliable);
        assertEquals(4, wire.requests.size());
        sink.queued.remove(0).invoke(new NullFederateAmbassador());
        service.requestAttributeTransportationTypeChange(object, attributes(51), reliable);
        assertThrows(AttributeAlreadyBeingChanged.class,
                () -> service.requestAttributeTransportationTypeChange(object, attributes(52), bestEffort));
    }

    @Test void interactionPendingIsPerClassAndEndsBeforeUserCallback() throws Exception {
        service.requestInteractionTransportationTypeChange(interaction, bestEffort);
        assertThrows(InteractionClassAlreadyBeingChanged.class,
                () -> service.requestInteractionTransportationTypeChange(interaction, reliable));
        service.requestInteractionTransportationTypeChange(Handles.interactionClass(42), reliable);
        sink.queued.remove(0).invoke(new NullFederateAmbassador() {
            @Override public void confirmInteractionTransportationTypeChange(InteractionClassHandle target, TransportationTypeHandle type) {
                assertDoesNotThrow(() -> service.requestInteractionTransportationTypeChange(target, reliable));
            }
        });
        assertEquals(5, wire.requests.size());
    }

    @Test void callbackExceptionDoesNotLeaveAttributePending() throws Exception {
        service.requestAttributeTransportationTypeChange(object, attributes(51), bestEffort);
        assertThrows(FederateInternalError.class, () -> sink.queued.remove(0).invoke(new NullFederateAmbassador() {
            @Override public void confirmAttributeTransportationTypeChange(ObjectInstanceHandle target, AttributeHandleSet attrs, TransportationTypeHandle type) throws FederateInternalError {
                throw new FederateInternalError("application rejected callback");
            }
        }));
        assertDoesNotThrow(() -> service.requestAttributeTransportationTypeChange(object, attributes(51), reliable));
    }

    @Test void failureQueuesNoConfirmationAndDoesNotCreatePendingState() throws Exception {
        wire.fail("AttributeNotOwned");
        assertThrows(AttributeNotOwned.class, () -> service.requestAttributeTransportationTypeChange(object, attributes(51), bestEffort));
        wire.fail("InteractionClassNotPublished");
        assertThrows(InteractionClassNotPublished.class, () -> service.requestInteractionTransportationTypeChange(interaction, bestEffort));
        assertTrue(sink.queued.isEmpty());
        wire.failure = null;
        service.requestAttributeTransportationTypeChange(object, attributes(51), reliable);
        service.requestInteractionTransportationTypeChange(interaction, reliable);
        assertEquals(2, sink.queued.size());
    }

    @Test void pendingDoesNotBlockOrdersOrQueries() throws Exception {
        service.requestAttributeTransportationTypeChange(object, attributes(51), bestEffort);
        service.requestInteractionTransportationTypeChange(interaction, bestEffort);
        service.changeAttributeOrderType(object, attributes(51), OrderType.TIMESTAMP);
        service.changeInteractionOrderType(interaction, OrderType.RECEIVE);
        wire.response = response(2);
        service.queryAttributeTransportationType(object, attribute);
        service.queryInteractionTransportationType(Handles.federate(7), interaction);
        assertEquals(6, wire.requests.size());
    }

    @Test void lifecycleClearSeparatesNewPendingFromOldCallbacks() throws Exception {
        service.requestAttributeTransportationTypeChange(object, attributes(51), bestEffort);
        service.requestInteractionTransportationTypeChange(interaction, bestEffort);
        List<CallbackSink.Invocation> old = new ArrayList<>(sink.queued);
        sink.queued.clear();
        service.clear();
        wire.prepared.clear();
        service.requestAttributeTransportationTypeChange(object, attributes(51), reliable);
        service.requestInteractionTransportationTypeChange(interaction, reliable);
        for (CallbackSink.Invocation callback : old) callback.invoke(new NullFederateAmbassador());
        assertThrows(AttributeAlreadyBeingChanged.class,
                () -> service.requestAttributeTransportationTypeChange(object, attributes(51), bestEffort));
        assertThrows(InteractionClassAlreadyBeingChanged.class,
                () -> service.requestInteractionTransportationTypeChange(interaction, bestEffort));
    }

    @Test void capturesLifecycleBeforeEachResponseProducingRpc() throws Exception {
        wire.beforeReply = () -> sink.epoch++;
        service.requestAttributeTransportationTypeChange(object, attributes(51), bestEffort);
        service.requestInteractionTransportationTypeChange(interaction, bestEffort);
        wire.response = response(1);
        service.queryAttributeTransportationType(object, attribute);
        service.queryInteractionTransportationType(Handles.federate(8), interaction);
        assertEquals(4, sink.captures);
        assertTrue(sink.queued.isEmpty(), "Responses from retired lifecycle must not enter the current callback queue");
    }

    @Test void clearDuringRpcDoesNotPoisonNewLifecyclePending() throws Exception {
        wire.beforeReply = () -> { service.clear(); wire.prepared.clear(); };
        service.requestAttributeTransportationTypeChange(object, attributes(51), bestEffort);
        service.requestInteractionTransportationTypeChange(interaction, bestEffort);
        wire.beforeReply = () -> { };
        assertDoesNotThrow(() -> service.requestAttributeTransportationTypeChange(object, attributes(51), reliable));
        assertDoesNotThrow(() -> service.requestInteractionTransportationTypeChange(interaction, reliable));
    }

    @Test void queryRejectsUnspecifiedAndUnknownWireTransportationWithoutInventingReport() {
        for (int type : new int[]{0, 99}) {
            wire.response = response(type);
            assertThrows(RTIinternalError.class, () -> service.queryAttributeTransportationType(object, attribute));
            assertThrows(RTIinternalError.class, () -> service.queryInteractionTransportationType(Handles.federate(8), interaction));
        }
        assertTrue(sink.queued.isEmpty());
    }

    @Test void allSixServicesPreserveAuthoritativeErrorsAndNeverQueueFailure() {
        for (String failure : List.of("SaveInProgress", "RestoreInProgress", "NotConnected", "FederateNotExecutionMember", "RTIinternalError")) {
            wire.fail(failure);
            for (Action action : actions()) assertEquals(failure, assertThrows(RTIexception.class, action::run).getClass().getSimpleName());
        }
        wire.fail("AttributeAlreadyBeingChanged");
        assertThrows(AttributeAlreadyBeingChanged.class, () -> service.requestAttributeTransportationTypeChange(object, attributes(51), reliable));
        wire.fail("InteractionClassAlreadyBeingChanged");
        assertThrows(InteractionClassAlreadyBeingChanged.class, () -> service.requestInteractionTransportationTypeChange(interaction, reliable));
        assertTrue(sink.queued.isEmpty());
    }

    @Test void nonmembersAndLocallyUnknownObjectsFailBeforeRpc() {
        context.member = false;
        for (Action action : actions()) assertThrows(FederateNotExecutionMember.class, action::run);
        context.member = true;
        exchange.clearKnowledge();
        assertThrows(ObjectInstanceNotKnown.class, () -> service.changeAttributeOrderType(object, attributes(51), OrderType.RECEIVE));
        assertThrows(ObjectInstanceNotKnown.class, () -> service.requestAttributeTransportationTypeChange(object, attributes(51), reliable));
        assertThrows(ObjectInstanceNotKnown.class, () -> service.queryAttributeTransportationType(object, attribute));
        assertTrue(wire.requests.isEmpty());
    }

    @Test void invalidArgumentsBecomeDeclaredCheckedFailuresWithoutRpc() {
        AttributeHandleSet invalid = new ForeignAttributes(); invalid.add(null);
        assertThrows(AttributeNotDefined.class, () -> service.changeAttributeOrderType(object, invalid, OrderType.RECEIVE));
        assertThrows(AttributeNotDefined.class, () -> service.requestAttributeTransportationTypeChange(object, null, reliable));
        assertThrows(AttributeNotDefined.class, () -> service.queryAttributeTransportationType(object, null));
        assertThrows(InteractionClassNotDefined.class, () -> service.changeInteractionOrderType(null, OrderType.RECEIVE));
        assertThrows(InteractionClassNotDefined.class, () -> service.requestInteractionTransportationTypeChange(null, reliable));
        assertThrows(InteractionClassNotDefined.class, () -> service.queryInteractionTransportationType(Handles.federate(8), null));
        assertThrows(RTIinternalError.class, () -> service.changeAttributeOrderType(object, attributes(51), null));
        assertThrows(RTIinternalError.class, () -> service.changeInteractionOrderType(interaction, null));
        assertThrows(RTIinternalError.class, () -> service.queryInteractionTransportationType(null, interaction));
        assertThrows(InvalidTransportationType.class, () -> service.requestAttributeTransportationTypeChange(object, attributes(51), null));
        assertThrows(InvalidTransportationType.class, () -> service.requestInteractionTransportationTypeChange(interaction, null));
        assertTrue(wire.requests.isEmpty());
    }

    @Test void emptyAttributeChangeIsExplicitAndDoesNotBlockUnrelatedAttributes() throws Exception {
        service.requestAttributeTransportationTypeChange(object, attributes(), reliable);
        service.requestAttributeTransportationTypeChange(object, attributes(51), bestEffort);
        assertTrue(((rti.v1.Object.ChangeAttributeTransportRequest) wire.requests.get(0)).getAttributeHandlesList().isEmpty());
        sink.queued.remove(0).invoke(new NullFederateAmbassador() {
            @Override public void confirmAttributeTransportationTypeChange(ObjectInstanceHandle target, AttributeHandleSet attrs, TransportationTypeHandle type) {
                assertEquals(object, target); assertTrue(attrs.isEmpty()); assertEquals(reliable, type);
            }
        });
        assertThrows(AttributeAlreadyBeingChanged.class, () -> service.requestAttributeTransportationTypeChange(object, attributes(51), reliable));
    }

    @Test void activationOccursOnlyAtCallbackEntryAndUsesAuthoritativeResult() throws Exception {
        service.requestAttributeTransportationTypeChange(object, attributes(51), bestEffort);
        assertEquals(List.of("PrepareAttributeTransportationTypeChange"), wire.methods);
        wire.achieved = Common.TransportationType.TRANSPORTATION_TYPE_RELIABLE;
        sink.queued.remove(0).invoke(new NullFederateAmbassador() {
            @Override public void confirmAttributeTransportationTypeChange(ObjectInstanceHandle target, AttributeHandleSet attrs, TransportationTypeHandle type) {
                assertEquals("ActivateTransportationTypeChange", wire.methods.get(wire.methods.size() - 1));
                assertEquals(reliable, type);
            }
        });
        rti.v1.Object.ActivateTransportationTypeChangeRequest activation = (rti.v1.Object.ActivateTransportationTypeChangeRequest) wire.requests.get(1);
        assertEquals(9, activation.getExpectedFederationGeneration());
        assertEquals(3, activation.getExpectedCallbackEpoch());
        assertEquals(32, activation.getTicket().size());
    }

    @Test void failedActivationNeverInvokesSuccessAndRetainsServerPending() throws Exception {
        service.requestInteractionTransportationTypeChange(interaction, reliable);
        wire.fail("RTIinternalError");
        boolean[] invoked = {false};
        assertThrows(RTIinternalError.class, () -> sink.queued.remove(0).invoke(new NullFederateAmbassador() {
            @Override public void confirmInteractionTransportationTypeChange(InteractionClassHandle target, TransportationTypeHandle type) { invoked[0] = true; }
        }));
        assertFalse(invoked[0]);
        wire.failure = null;
        assertThrows(InteractionClassAlreadyBeingChanged.class, () -> service.requestInteractionTransportationTypeChange(interaction, bestEffort));
    }

    @Test void serverOwnershipLossDoesNotLeaveLocalPendingOverride() throws Exception {
        service.requestAttributeTransportationTypeChange(object, attributes(51), reliable);
        wire.prepared.replaceAll((ticket, change) -> change.toBuilder().clearAttributeHandles().build());
        assertDoesNotThrow(() -> service.requestAttributeTransportationTypeChange(object, attributes(51), bestEffort));
        sink.queued.remove(0).invoke(new NullFederateAmbassador() {
            @Override public void confirmAttributeTransportationTypeChange(ObjectInstanceHandle target, AttributeHandleSet attrs, TransportationTypeHandle type) {
                assertTrue(attrs.isEmpty());
            }
        });
        assertThrows(AttributeAlreadyBeingChanged.class, () -> service.requestAttributeTransportationTypeChange(object, attributes(51), reliable));
    }

    @Test void ownershipLossConfirmationAcceptsOnlySubsetOfPreparedAttributes() throws Exception {
        service.requestAttributeTransportationTypeChange(object, attributes(51, 52), reliable);
        wire.prepared.replaceAll((ticket, change) -> change.toBuilder().clearAttributeHandles().addAttributeHandles(52).build());
        sink.queued.remove(0).invoke(new NullFederateAmbassador() {
            @Override public void confirmAttributeTransportationTypeChange(ObjectInstanceHandle target, AttributeHandleSet attrs, TransportationTypeHandle type) {
                assertEquals(attributes(52), attrs);
            }
        });
        service.requestAttributeTransportationTypeChange(object, attributes(51), reliable);
        wire.prepared.replaceAll((ticket, change) -> change.toBuilder().addAttributeHandles(99).build());
        assertThrows(RTIinternalError.class, () -> sink.queued.remove(0).invoke(new NullFederateAmbassador()));
    }

    @Test void mismatchedActivationPayloadNeverInvokesConfirmation() throws Exception {
        service.requestInteractionTransportationTypeChange(interaction, reliable);
        wire.corruptActivation = true;
        assertThrows(RTIinternalError.class, () -> sink.queued.remove(0).invoke(new NullFederateAmbassador() {
            @Override public void confirmInteractionTransportationTypeChange(InteractionClassHandle target, TransportationTypeHandle type) { fail("mismatched result must not be confirmed"); }
        }));
    }

    @Test void checkpointAdmissionDefersActivationWithoutLosingConfirmation() throws Exception {
        service.requestInteractionTransportationTypeChange(interaction, reliable);
        for (String name : List.of("SaveInProgress", "RestoreInProgress")) {
            wire.fail(name);
            assertDoesNotThrow(() -> sink.queued.remove(0).invoke(new NullFederateAmbassador() {
                @Override public void confirmInteractionTransportationTypeChange(InteractionClassHandle target, TransportationTypeHandle type) { fail("checkpoint-blocked activation must not confirm"); }
            }));
            assertEquals(1, sink.queued.size());
        }
        wire.failure = null;
        sink.queued.remove(0).invoke(new NullFederateAmbassador());
        assertTrue(sink.queued.isEmpty());
    }

    @Test void lostActivationReplyCanRetrySameTicketWithoutEarlySuccess() throws Exception {
        service.requestInteractionTransportationTypeChange(interaction, reliable);
        wire.failure = Status.UNAVAILABLE;
        assertThrows(RTIinternalError.class, () -> sink.queued.remove(0).invoke(new NullFederateAmbassador()));
        assertEquals(1, sink.queued.size());
        Message first = wire.requests.get(1);
        wire.failure = null;
        sink.queued.remove(0).invoke(new NullFederateAmbassador());
        assertEquals(first, wire.requests.get(2));
    }

    private List<Action> actions() {
        return List.of(() -> service.changeAttributeOrderType(object, attributes(51), OrderType.RECEIVE),
                () -> service.changeInteractionOrderType(interaction, OrderType.TIMESTAMP),
                () -> service.requestAttributeTransportationTypeChange(object, attributes(51), reliable),
                () -> service.requestInteractionTransportationTypeChange(interaction, reliable),
                () -> service.queryAttributeTransportationType(object, attribute),
                () -> service.queryInteractionTransportationType(Handles.federate(8), interaction));
    }

    private static AttributeHandleSet attributes(long... values) {
        AttributeHandleSet result = HandleCollections.attributeSetFactory().create();
        for (long value : values) result.add(Handles.attribute(value));
        return result;
    }
    private static Message response(int transport) { return rti.v1.Object.TransportationTypeResponse.newBuilder().setTransportTypeValue(transport).build(); }
    private static Object field(Message value, String name) { return value.getField(value.getDescriptorForType().findFieldByName(name)); }
    @FunctionalInterface private interface Action { void run() throws RTIexception; }
    private static final class ForeignAttributes extends HashSet<AttributeHandle> implements AttributeHandleSet {
        @Override public ForeignAttributes clone() { return (ForeignAttributes) super.clone(); }
    }

    private static final class CapturingSink implements CallbackSink {
        final List<Invocation> queued = new ArrayList<>();
        int captures;
        int epoch;
        public void enqueue(Invocation callback) { fail("Must enqueue using captured callback sink"); }
        public CallbackSink capture() {
            captures++;
            int captured = epoch;
            return callback -> { if (captured == epoch) queued.add(callback); };
        }
    }

    private static final class Context implements ServiceContext {
        private final Channel wire;
        boolean member = true;
        Context(Channel wire) { this.wire = wire; }
        public void requireMember() throws RTIexception { if (!member) throw new FederateNotExecutionMember("not joined"); }
        public Channel channel() { return wire; }
        public String federation() { return "fed"; }
        public long federate() { return 7; }
        public long generation() { return 9; }
        public boolean regulating() { return false; }
        public double logicalTime() { return 0; }
    }

    private static final class RecordingChannel extends Channel {
        final List<Message> requests = new ArrayList<>();
        final List<String> methods = new ArrayList<>();
        final Map<ByteString, rti.v1.Object.TransportationChange> prepared = new HashMap<>();
        Common.TransportationType achieved;
        boolean corruptActivation;
        int nextTicket;
        Message response = Common.Empty.getDefaultInstance();
        Status failure;
        Metadata trailers = new Metadata();
        Runnable beforeReply = () -> { };
        void fail(String name) {
            failure = Status.FAILED_PRECONDITION.withDescription("authoritative server rejection");
            trailers = new Metadata();
            trailers.put(Metadata.Key.of("rti-spec-exception", Metadata.ASCII_STRING_MARSHALLER), name);
        }
        public String authority() { return "transportation-test"; }
        public <ReqT, RespT> ClientCall<ReqT, RespT> newCall(MethodDescriptor<ReqT, RespT> method, CallOptions options) {
            return new ClientCall<ReqT, RespT>() {
                private Listener<RespT> listener;
                private Message sent;
                public void start(Listener<RespT> target, Metadata headers) { listener = target; }
                public void request(int count) { }
                public void cancel(String message, Throwable cause) { }
                public void sendMessage(ReqT request) { sent = (Message) request; requests.add(sent); methods.add(method.getBareMethodName()); }
                @SuppressWarnings("unchecked") public void halfClose() {
                    if (failure != null) { listener.onClose(failure, trailers); return; }
                    Message reply = response;
                    if (method.getBareMethodName().startsWith("Prepare")) {
                        byte[] token = new byte[32]; token[0] = (byte) ++nextTicket;
                        rti.v1.Object.TransportationChange.Builder change = rti.v1.Object.TransportationChange.newBuilder()
                                .setTicket(ByteString.copyFrom(token)).setFederationGeneration(9).setCallbackEpoch(3);
                        if (sent instanceof rti.v1.Object.ChangeAttributeTransportRequest) {
                            rti.v1.Object.ChangeAttributeTransportRequest req = (rti.v1.Object.ChangeAttributeTransportRequest) sent;
                            for (rti.v1.Object.TransportationChange p : prepared.values()) {
                                if (p.getObjectHandle() == req.getObjectHandle() && req.getAttributeHandlesList().stream().anyMatch(p.getAttributeHandlesList()::contains)) {
                                    reject("AttributeAlreadyBeingChanged"); return;
                                }
                            }
                            change.setObjectHandle(req.getObjectHandle()).addAllAttributeHandles(req.getAttributeHandlesList()).setTransportType(req.getTransportType());
                        } else {
                            rti.v1.Object.ChangeInteractionTransportRequest req = (rti.v1.Object.ChangeInteractionTransportRequest) sent;
                            if (prepared.values().stream().anyMatch(p -> p.getInteractionClassHandle() == req.getInteractionClassHandle())) {
                                reject("InteractionClassAlreadyBeingChanged"); return;
                            }
                            change.setInteractionClassHandle(req.getInteractionClassHandle()).setTransportType(req.getTransportType());
                        }
                        reply = change.build(); prepared.put(change.getTicket(), (rti.v1.Object.TransportationChange) reply);
                    } else if (sent instanceof rti.v1.Object.ActivateTransportationTypeChangeRequest) {
                        rti.v1.Object.TransportationChange change = prepared.remove(((rti.v1.Object.ActivateTransportationTypeChangeRequest) sent).getTicket());
                        if (change == null) { reject("RTIinternalError"); return; }
                        if (achieved != null) change = change.toBuilder().setTransportType(achieved).build();
                        if (corruptActivation) change = change.toBuilder().setInteractionClassHandle(999).build();
                        reply = change;
                    }
                    beforeReply.run();
                    listener.onMessage((RespT) reply);
                    listener.onClose(Status.OK, new Metadata());
                }
                private void reject(String name) {
                    Metadata trailer = new Metadata();
                    trailer.put(Metadata.Key.of("rti-spec-exception", Metadata.ASCII_STRING_MARSHALLER), name);
                    listener.onClose(Status.FAILED_PRECONDITION, trailer);
                }
            };
        }
    }
}
