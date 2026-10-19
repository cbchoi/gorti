package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;

import com.google.protobuf.ByteString;
import com.google.protobuf.Message;
import hla.rti1516e.*;
import hla.rti1516e.exceptions.*;
import io.grpc.*;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Set;
import org.gorti.rti.types.HandleCollections;
import org.gorti.rti.types.Handles;
import org.junit.jupiter.api.Test;
import rti.v1.Common;
import rti.v1.Ownership;

class OwnershipServicesTest {
    private final RecordingChannel wire = new RecordingChannel();
    private final Context context = new Context(wire);
    private final ExchangeServices exchange = new ExchangeServices(context);
    private final List<CallbackSink.Invocation> queued = new ArrayList<>();
    private final OwnershipServices ownership = new OwnershipServices(context, queued::add, exchange);
    private final ObjectInstanceHandle object = Handles.objectInstance(31);
    private final AttributeHandle attribute = Handles.attribute(51);

    @Test void everyMethodUsesItsExactRpcAndJoinedEnvelope() throws Exception {
        exchange.discovered(31, 21, "item");
        AttributeHandleSet attributes = attributes(51, 52);
        byte[] tag = {0, -1, 7};
        ownership.unconditionalAttributeOwnershipDivestiture(object, attributes);
        ownership.negotiatedAttributeOwnershipDivestiture(object, attributes, tag);
        Ownership.NegotiatedDivestRequest negotiated = last(Ownership.NegotiatedDivestRequest.class);
        assertTrue(negotiated.getTwoPhase());
        ownership.confirmDivestiture(object, attributes, tag);
        Ownership.ConfirmDivestitureRequest confirmation = last(Ownership.ConfirmDivestitureRequest.class);
        ownership.attributeOwnershipAcquisition(object, attributes, tag);
        Ownership.AcquireRequest acquire = last(Ownership.AcquireRequest.class);
        assertFalse(acquire.getIfAvailable());
        ownership.attributeOwnershipAcquisitionIfAvailable(object, attributes);
        assertTrue(last(Ownership.AcquireRequest.class).getIfAvailable());
        assertTrue(last(Ownership.AcquireRequest.class).getTag().isEmpty());
        ownership.attributeOwnershipReleaseDenied(object, attributes);
        wire.response = Ownership.DivestIfWantedResponse.newBuilder().addDivestedAttributeHandles(52).build();
        assertEquals(Set.of(Handles.attribute(52)), ownership.attributeOwnershipDivestitureIfWanted(object, attributes));
        wire.response = Common.Empty.getDefaultInstance();
        ownership.cancelNegotiatedAttributeOwnershipDivestiture(object, attributes);
        ownership.cancelAttributeOwnershipAcquisition(object, attributes);
        wire.response = Ownership.QueryOwnershipResponse.newBuilder().setOwned(true).setOwnerFederateHandle(8).build();
        ownership.queryAttributeOwnership(object, attribute);
        wire.response = Ownership.IsOwnedResponse.newBuilder().setOwned(true).build();
        assertTrue(ownership.isAttributeOwnedByFederate(object, attribute));
        tag[0] = 99;
        for (ByteString sent : List.of(negotiated.getTag(), confirmation.getTag(), acquire.getTag())) {
            assertEquals(ByteString.copyFrom(new byte[]{0, -1, 7}), sent);
        }
        assertEquals(List.of("UnconditionalAttributeOwnershipDivestiture", "NegotiatedAttributeOwnershipDivestiture",
                "ConfirmDivestiture", "AttributeOwnershipAcquisition", "AttributeOwnershipAcquisition",
                "AttributeOwnershipReleaseDenied", "AttributeOwnershipDivestitureIfWantedWithResult",
                "CancelNegotiatedAttributeOwnershipDivestiture", "CancelAttributeOwnershipAcquisition",
                "QueryAttributeOwnership", "IsAttributeOwnedByFederate"), wire.methods);
        for (Message request : wire.requests) {
            assertEquals(Common.WireVersion.WIRE_VERSION_V1.getValueDescriptor(), field(request, "wire_version"));
            assertEquals("fed", field(request, "federation_name"));
            assertEquals(7L, field(request, "federate_handle"));
            assertEquals(31L, field(request, "object_handle"));
            if (request.getDescriptorForType().findFieldByName("attribute_handles") != null) {
                assertEquals(Set.of(51L, 52L), Set.copyOf((List<?>) field(request, "attribute_handles")));
            } else assertEquals(51L, field(request, "attribute_handle"));
        }
        assertEquals(2, queued.size());
    }

    @Test void successfulCancellationQueuesCopiedRequestOnlyAfterRpcCompletion() throws Exception {
        exchange.discovered(31, 21, "item");
        AttributeHandleSet request = attributes(51, 52);
        ownership.cancelAttributeOwnershipAcquisition(object, request);
        request.clear();
        assertEquals(1, queued.size());
        List<String> delivered = new ArrayList<>();
        queued.remove(0).invoke(new NullFederateAmbassador() {
            @Override public void confirmAttributeOwnershipAcquisitionCancellation(ObjectInstanceHandle target, AttributeHandleSet attributes) {
                assertEquals(object, target);
                assertEquals(Set.of(attribute, Handles.attribute(52)), attributes);
                delivered.add("cancelled");
            }
        });
        assertEquals(List.of("cancelled"), delivered);
    }

    @Test void queryCallbacksDescribeTheRpcSnapshotNotLaterOwnership() throws Exception {
        exchange.discovered(31, 21, "item");
        wire.response = Ownership.QueryOwnershipResponse.newBuilder().setOwned(true).setOwnerFederateHandle(8).build();
        ownership.queryAttributeOwnership(object, attribute);
        wire.response = Ownership.QueryOwnershipResponse.getDefaultInstance();
        ownership.queryAttributeOwnership(object, attribute);
        List<String> delivered = new ArrayList<>();
        NullFederateAmbassador callback = new NullFederateAmbassador() {
            @Override public void informAttributeOwnership(ObjectInstanceHandle target, AttributeHandle attr, FederateHandle owner) {
                assertEquals(object, target); assertEquals(attribute, attr); assertEquals(Handles.federate(8), owner);
                delivered.add("owned");
            }
            @Override public void attributeIsNotOwned(ObjectInstanceHandle target, AttributeHandle attr) {
                assertEquals(object, target); assertEquals(attribute, attr); delivered.add("unowned");
            }
        };
        assertTrue(delivered.isEmpty());
        for (CallbackSink.Invocation invocation : queued) invocation.invoke(callback);
        assertEquals(List.of("owned", "unowned"), delivered);
        assertEquals(2, wire.requests.size());
    }

    @Test void contradictoryOwnershipResultsNeverQueueCallbacks() {
        exchange.discovered(31, 21, "item");
        wire.response = Ownership.QueryOwnershipResponse.newBuilder().setOwned(true).build();
        assertThrows(RTIinternalError.class, () -> ownership.queryAttributeOwnership(object, attribute));
        wire.response = Ownership.QueryOwnershipResponse.newBuilder().setOwnerFederateHandle(8).build();
        assertThrows(RTIinternalError.class, () -> ownership.queryAttributeOwnership(object, attribute));
        wire.response = Ownership.QueryOwnershipResponse.newBuilder().setOwnerFederateHandle(-1L).build();
        assertThrows(RTIinternalError.class, () -> ownership.queryAttributeOwnership(object, attribute));
        assertTrue(queued.isEmpty());
    }

    @Test void reservedRtiOwnerQueuesRtiCallbackWithoutFederateHandle() throws Exception {
        exchange.discovered(31, 21, "item");
        wire.response = Ownership.QueryOwnershipResponse.newBuilder().setOwned(true).setOwnerFederateHandle(-1L).build();
        ownership.queryAttributeOwnership(object, attribute);
        List<String> delivered = new ArrayList<>();
        assertEquals(1, queued.size());
        queued.get(0).invoke(new NullFederateAmbassador() {
            @Override public void attributeIsOwnedByRTI(ObjectInstanceHandle target, AttributeHandle attr) {
                assertEquals(object, target); assertEquals(attribute, attr); delivered.add("rti");
            }
            @Override public void informAttributeOwnership(ObjectInstanceHandle target, AttributeHandle attr, FederateHandle owner) {
                fail("The internal MOM owner is not a joined federate");
            }
        });
        assertEquals(List.of("rti"), delivered);
    }

    @Test void ifWantedReturnsOnlyCommittedResultsWithoutQueriesOrFallback() throws Exception {
        exchange.discovered(31, 21, "item");
        wire.response = Ownership.DivestIfWantedResponse.getDefaultInstance();
        AttributeHandleSet result = ownership.attributeOwnershipDivestitureIfWanted(object, attributes(51));
        assertTrue(result.isEmpty());
        result.add(attribute);
        assertTrue(ownership.attributeOwnershipDivestitureIfWanted(object, attributes(51)).isEmpty());
        assertEquals(List.of("AttributeOwnershipDivestitureIfWantedWithResult", "AttributeOwnershipDivestitureIfWantedWithResult"), wire.methods);
        wire.failure = Status.UNIMPLEMENTED;
        assertThrows(RTIinternalError.class, () -> ownership.attributeOwnershipDivestitureIfWanted(object, attributes(51)));
        assertEquals(3, wire.methods.size());
        assertTrue(queued.isEmpty());
    }

    @Test void everyServiceRejectsUnknownLocalObjectsAndNonmembersBeforeRpc() throws Exception {
        for (Action action : actions()) assertThrows(ObjectInstanceNotKnown.class, action::run);
        exchange.discovered(31, 21, "item");
        exchange.clearKnowledge();
        for (Action action : actions()) assertThrows(ObjectInstanceNotKnown.class, action::run);
        context.member = false;
        for (Action action : actions()) assertThrows(FederateNotExecutionMember.class, action::run);
        assertTrue(wire.requests.isEmpty());
        assertTrue(queued.isEmpty());
    }

    @Test void invalidInputsAreCheckedFailuresWithoutWireCalls() {
        exchange.discovered(31, 21, "item");
        AttributeHandleSet nullMember = new ForeignAttributes();
        nullMember.add(null);
        assertThrows(ObjectInstanceNotKnown.class, () -> ownership.queryAttributeOwnership(null, attribute));
        assertThrows(AttributeNotDefined.class, () -> ownership.queryAttributeOwnership(object, null));
        assertThrows(AttributeNotDefined.class, () -> ownership.isAttributeOwnedByFederate(object, null));
        assertThrows(AttributeNotDefined.class, () -> ownership.unconditionalAttributeOwnershipDivestiture(object, null));
        assertThrows(AttributeNotDefined.class, () -> ownership.attributeOwnershipAcquisition(object, nullMember, new byte[0]));
        assertThrows(RTIinternalError.class, () -> ownership.negotiatedAttributeOwnershipDivestiture(object, attributes(51), null));
        assertThrows(RTIinternalError.class, () -> ownership.confirmDivestiture(object, attributes(51), null));
        assertThrows(RTIinternalError.class, () -> ownership.attributeOwnershipAcquisition(object, attributes(51), null));
        assertTrue(wire.requests.isEmpty());
    }

    @Test void authoritativeCheckedErrorsArePreservedByEveryService() throws Exception {
        exchange.discovered(31, 21, "item");
        List<Class<? extends RTIexception>> errors = List.of(AttributeNotOwned.class, AttributeAlreadyBeingDivested.class,
                NoAcquisitionPending.class, FederateOwnsAttributes.class, AttributeAlreadyBeingAcquired.class,
                AttributeNotOwned.class, AttributeNotOwned.class, AttributeDivestitureWasNotRequested.class,
                AttributeAcquisitionWasNotRequested.class, AttributeNotDefined.class, ObjectInstanceNotKnown.class);
        List<Action> actions = actions();
        for (int i = 0; i < actions.size(); i++) {
            wire.fail(errors.get(i).getSimpleName());
            assertThrows(errors.get(i), actions.get(i)::run);
        }
        for (String name : List.of("SaveInProgress", "RestoreInProgress", "NotConnected", "ObjectClassNotPublished", "AttributeNotPublished")) {
            wire.fail(name);
            assertEquals(name, assertThrows(RTIexception.class, () -> ownership.attributeOwnershipAcquisition(object, attributes(51), new byte[0])).getClass().getSimpleName());
        }
        assertTrue(queued.isEmpty());
    }

    @Test void ambiguousCancellationFailureDoesNotGuessExceptionOrQueueSuccess() {
        exchange.discovered(31, 21, "item");
        wire.failure = Status.FAILED_PRECONDITION.withDescription("ownership: not in transfer");
        assertThrows(RTIinternalError.class, () -> ownership.cancelAttributeOwnershipAcquisition(object, attributes(51)));
        assertThrows(RTIinternalError.class, () -> ownership.cancelNegotiatedAttributeOwnershipDivestiture(object, attributes(51)));
        assertTrue(queued.isEmpty());
    }

    private List<Action> actions() {
        AttributeHandleSet a = attributes(51);
        return List.of(() -> ownership.unconditionalAttributeOwnershipDivestiture(object, a),
                () -> ownership.negotiatedAttributeOwnershipDivestiture(object, a, new byte[0]),
                () -> ownership.confirmDivestiture(object, a, new byte[0]),
                () -> ownership.attributeOwnershipAcquisition(object, a, new byte[0]),
                () -> ownership.attributeOwnershipAcquisitionIfAvailable(object, a),
                () -> ownership.attributeOwnershipReleaseDenied(object, a),
                () -> ownership.attributeOwnershipDivestitureIfWanted(object, a),
                () -> ownership.cancelNegotiatedAttributeOwnershipDivestiture(object, a),
                () -> ownership.cancelAttributeOwnershipAcquisition(object, a),
                () -> ownership.queryAttributeOwnership(object, attribute),
                () -> ownership.isAttributeOwnedByFederate(object, attribute));
    }

    private static AttributeHandleSet attributes(long... handles) {
        AttributeHandleSet result = HandleCollections.attributeSetFactory().create();
        for (long handle : handles) result.add(Handles.attribute(handle));
        return result;
    }
    private <T> T last(Class<T> type) { return type.cast(wire.requests.get(wire.requests.size() - 1)); }
    private static Object field(Message value, String name) { return value.getField(value.getDescriptorForType().findFieldByName(name)); }
    @FunctionalInterface private interface Action { void run() throws RTIexception; }

    private static final class ForeignAttributes extends HashSet<AttributeHandle> implements AttributeHandleSet {
        @Override public ForeignAttributes clone() { return (ForeignAttributes) super.clone(); }
    }

    private static final class Context implements ServiceContext {
        private final Channel wire;
        boolean member = true;
        Context(Channel wire) { this.wire = wire; }
        public void requireMember() throws RTIexception { if (!member) throw new FederateNotExecutionMember("not joined"); }
        public Channel channel() { assertTrue(member); return wire; }
        public String federation() { return "fed"; }
        public long federate() { return 7; }
        public long generation() { return 9; }
        public boolean regulating() { return false; }
        public double logicalTime() { return 0; }
    }

    private static final class RecordingChannel extends Channel {
        final List<Message> requests = new ArrayList<>();
        final List<String> methods = new ArrayList<>();
        Message response = Common.Empty.getDefaultInstance();
        Status failure;
        Metadata trailers = new Metadata();
        void fail(String name) {
            failure = Status.FAILED_PRECONDITION.withDescription("authoritative server rejection");
            trailers = new Metadata();
            trailers.put(Metadata.Key.of("rti-spec-exception", Metadata.ASCII_STRING_MARSHALLER), name);
        }
        public String authority() { return "ownership-test"; }
        public <ReqT, RespT> ClientCall<ReqT, RespT> newCall(MethodDescriptor<ReqT, RespT> method, CallOptions options) {
            return new ClientCall<ReqT, RespT>() {
                private Listener<RespT> listener;
                public void start(Listener<RespT> target, Metadata headers) { listener = target; }
                public void request(int count) { }
                public void cancel(String message, Throwable cause) { }
                public void sendMessage(ReqT request) { requests.add((Message) request); methods.add(method.getBareMethodName()); }
                @SuppressWarnings("unchecked") public void halfClose() {
                    if (failure != null) { listener.onClose(failure, trailers); return; }
                    listener.onMessage((RespT) response);
                    listener.onClose(Status.OK, new Metadata());
                }
            };
        }
    }
}
