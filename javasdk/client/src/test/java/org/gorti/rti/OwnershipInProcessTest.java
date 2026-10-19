package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;

import hla.rti1516e.*;
import hla.rti1516e.exceptions.*;
import io.grpc.*;
import io.grpc.inprocess.InProcessChannelBuilder;
import io.grpc.inprocess.InProcessServerBuilder;
import io.grpc.stub.StreamObserver;
import java.util.ArrayList;
import java.util.List;
import java.util.Set;
import java.util.concurrent.TimeUnit;
import org.gorti.rti.types.HandleCollections;
import org.gorti.rti.types.Handles;
import org.junit.jupiter.api.Test;
import rti.v1.Common;
import rti.v1.Ownership;
import rti.v1.OwnershipServiceGrpc;

class OwnershipInProcessTest {
    @Test void transportResultsAreQueuedAndExactDivestResultUsesNoFollowupQuery() throws Exception {
        List<String> received = new ArrayList<>();
        try (Session session = new Session(new OwnershipServiceGrpc.OwnershipServiceImplBase() {
            @Override public void cancelAttributeOwnershipAcquisition(Ownership.CancelAcquireRequest request, StreamObserver<Common.Empty> reply) {
                assertEquals(List.of(51L), request.getAttributeHandlesList());
                received.add("cancel");
                reply.onNext(Common.Empty.getDefaultInstance()); reply.onCompleted();
            }
            @Override public void queryAttributeOwnership(Ownership.QueryOwnershipRequest request, StreamObserver<Ownership.QueryOwnershipResponse> reply) {
                assertEquals(7, request.getFederateHandle()); assertEquals("fed", request.getFederationName());
                received.add("query");
                reply.onNext(Ownership.QueryOwnershipResponse.newBuilder().setOwned(true).setOwnerFederateHandle(8).build()); reply.onCompleted();
            }
            @Override public void attributeOwnershipDivestitureIfWantedWithResult(Ownership.DivestIfWantedRequest request, StreamObserver<Ownership.DivestIfWantedResponse> reply) {
                received.add("divest-result");
                reply.onNext(Ownership.DivestIfWantedResponse.newBuilder().addDivestedAttributeHandles(51).build()); reply.onCompleted();
            }
        })) {
            List<String> callbacks = new ArrayList<>();
            session.ownership.cancelAttributeOwnershipAcquisition(session.object, session.attributes);
            session.ownership.queryAttributeOwnership(session.object, Handles.attribute(51));
            assertEquals(Set.of(Handles.attribute(51)), session.ownership.attributeOwnershipDivestitureIfWanted(session.object, session.attributes));
            assertEquals(List.of("cancel", "query", "divest-result"), received);
            assertTrue(callbacks.isEmpty());
            assertEquals(2, session.queued.size());
            for (CallbackSink.Invocation event : session.queued) event.invoke(new NullFederateAmbassador() {
                @Override public void confirmAttributeOwnershipAcquisitionCancellation(ObjectInstanceHandle object, AttributeHandleSet attributes) { callbacks.add("cancel"); }
                @Override public void informAttributeOwnership(ObjectInstanceHandle object, AttributeHandle attribute, FederateHandle owner) {
                    assertEquals(Handles.federate(8), owner); callbacks.add("query");
                }
            });
            assertEquals(List.of("cancel", "query"), callbacks);
        }
    }

    @Test void checkedExceptionTrailersSurviveRealGrpcAndNeverEnqueueSuccess() throws Exception {
        try (Session session = new Session(new OwnershipServiceGrpc.OwnershipServiceImplBase() {
            @Override public void cancelAttributeOwnershipAcquisition(Ownership.CancelAcquireRequest request, StreamObserver<Common.Empty> reply) {
                Metadata trailers = new Metadata();
                trailers.put(Metadata.Key.of("rti-spec-exception", Metadata.ASCII_STRING_MARSHALLER), "AttributeAcquisitionWasNotRequested");
                reply.onError(Status.FAILED_PRECONDITION.withDescription("cannot cancel").asRuntimeException(trailers));
            }
            @Override public void queryAttributeOwnership(Ownership.QueryOwnershipRequest request, StreamObserver<Ownership.QueryOwnershipResponse> reply) {
                reply.onError(Status.UNAVAILABLE.withDescription("backend stopped").asRuntimeException());
            }
        })) {
            assertThrows(AttributeAcquisitionWasNotRequested.class, () -> session.ownership.cancelAttributeOwnershipAcquisition(session.object, session.attributes));
            assertThrows(RTIinternalError.class, () -> session.ownership.queryAttributeOwnership(session.object, Handles.attribute(51)));
            assertTrue(session.queued.isEmpty());
        }
    }

    private static final class Session implements AutoCloseable, ServiceContext {
        final Server server;
        final ManagedChannel channel;
        final List<CallbackSink.Invocation> queued = new ArrayList<>();
        final ObjectInstanceHandle object = Handles.objectInstance(31);
        final AttributeHandleSet attributes = HandleCollections.attributeSetFactory().create();
        final OwnershipServices ownership;
        Session(BindableService service) throws Exception {
            String name = InProcessServerBuilder.generateName();
            server = InProcessServerBuilder.forName(name).directExecutor().addService(service).build().start();
            channel = InProcessChannelBuilder.forName(name).directExecutor().build();
            ExchangeServices exchange = new ExchangeServices(this);
            exchange.discovered(31, 21, "item");
            ownership = new OwnershipServices(this, queued::add, exchange);
            attributes.add(Handles.attribute(51));
        }
        public void requireMember() { }
        public Channel channel() { return channel; }
        public String federation() { return "fed"; }
        public long federate() { return 7; }
        public long generation() { return 9; }
        public boolean regulating() { return false; }
        public double logicalTime() { return 0; }
        public void close() throws InterruptedException {
            channel.shutdownNow(); server.shutdownNow();
            assertTrue(channel.awaitTermination(5, TimeUnit.SECONDS));
            assertTrue(server.awaitTermination(5, TimeUnit.SECONDS));
        }
    }
}
