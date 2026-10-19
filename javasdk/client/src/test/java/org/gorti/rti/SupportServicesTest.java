package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;

import com.google.protobuf.Message;
import hla.rti1516e.*;
import hla.rti1516e.exceptions.*;
import io.grpc.*;
import io.grpc.inprocess.InProcessChannelBuilder;
import io.grpc.inprocess.InProcessServerBuilder;
import io.grpc.stub.MetadataUtils;
import io.grpc.stub.StreamObserver;
import java.lang.reflect.Proxy;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.TimeUnit;
import org.gorti.rti.types.Handles;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.function.Executable;
import rti.v1.Common;
import rti.v1.Declaration;
import rti.v1.DeclarationServiceGrpc;
import rti.v1.Federation;
import rti.v1.FederationServiceGrpc;
import rti.v1.Support;
import rti.v1.SupportServiceGrpc;

class SupportServicesTest {
    @Test void queriesUseRpcResultsAndExactJoinedIdentity() throws Exception {
        try (Session s = new Session()) {
            s.rate = 12.5;
            assertEquals(12.5, s.support.getUpdateRateValue("fast"));
            assertEquals("fast", ((Support.GetUpdateRateValueRequest) s.last()).getUpdateRateDesignator());
            s.rate = 0;
            assertEquals(0, s.support.getUpdateRateValueForAttribute(Handles.objectInstance(31), Handles.attribute(51)));
            Support.GetUpdateRateValueForAttributeRequest request = (Support.GetUpdateRateValueForAttributeRequest) s.last();
            assertEquals(31, request.getObjectHandle());
            assertEquals(51, request.getAttributeHandle());
            s.normalized = 1234;
            long wideHandle = 0x1234567800000008L;
            assertEquals(1234, s.support.normalizeFederateHandle(Handles.federate(wideHandle)));
            assertEquals(wideHandle, ((Support.NormalizeFederateHandleRequest) s.last()).getTargetHandle());
            for (ServiceGroup group : ServiceGroup.values()) {
                s.normalized++;
                assertEquals(s.normalized, s.support.normalizeServiceGroup(group));
                assertEquals(group.ordinal(), ((Support.NormalizeServiceGroupRequest) s.last()).getServiceGroup());
            }
            assertEquals(10, s.requests.size());
        }
    }

    @Test void allSixResignActionsRoundTripWithExplicitWireValuesAndNoCache() throws Exception {
        try (Session s = new Session()) {
            int[] wireValues = {1, 6, 4, 2, 3, 5};
            for (ResignAction action : ResignAction.values()) {
                s.support.setAutomaticResignDirective(action);
                assertEquals(wireValues[action.ordinal()], s.action);
                assertEquals(action, s.support.getAutomaticResignDirective());
            }
            s.action = 6;
            assertEquals(ResignAction.DELETE_OBJECTS, s.support.getAutomaticResignDirective());
            s.action = 0;
            assertThrows(RTIinternalError.class, s.support::getAutomaticResignDirective);
            s.action = 999;
            assertThrows(RTIinternalError.class, s.support::getAutomaticResignDirective);
        }
    }

    @Test void invalidServerRateValuesAreRejectedInsteadOfPublished() throws Exception {
        try (Session s = new Session()) {
            for (double value : new double[]{-1, Double.NaN, Double.POSITIVE_INFINITY, Double.NEGATIVE_INFINITY}) {
                s.rate = value;
                assertThrows(RTIinternalError.class, () -> s.support.getUpdateRateValue("fast"));
                assertThrows(RTIinternalError.class, () -> s.support.getUpdateRateValueForAttribute(Handles.objectInstance(31), Handles.attribute(51)));
            }
        }
    }

    @Test void eachSwitchUsesTheCorrespondingServerSideMutation() throws Throwable {
        try (Session s = new Session()) {
            for (int i = 0; i < s.switches().size(); i++) {
                s.switches().get(i).execute();
                Declaration.SetAdvisorySwitchRequest request = (Declaration.SetAdvisorySwitchRequest) s.last();
                assertEquals(i / 2 + 1, request.getSwitchKindValue());
                assertEquals(i % 2 == 0, request.getEnabled());
            }
            assertEquals(8, s.requests.size());
        }
    }

    @Test void invalidArgumentsAndUnknownObjectsNeverReachTheServer() throws Exception {
        try (Session s = new Session()) {
            assertThrows(InvalidUpdateRateDesignator.class, () -> s.support.getUpdateRateValue(null));
            assertThrows(InvalidResignAction.class, () -> s.support.setAutomaticResignDirective(null));
            assertThrows(InvalidServiceGroup.class, () -> s.support.normalizeServiceGroup(null));
            assertThrows(InvalidFederateHandle.class, () -> s.support.normalizeFederateHandle(null));
            assertThrows(InvalidFederateHandle.class, () -> s.support.normalizeFederateHandle(foreign(FederateHandle.class)));
            assertThrows(ObjectInstanceNotKnown.class, () -> s.support.getUpdateRateValueForAttribute(null, Handles.attribute(51)));
            assertThrows(ObjectInstanceNotKnown.class, () -> s.support.getUpdateRateValueForAttribute(foreign(ObjectInstanceHandle.class), Handles.attribute(51)));
            assertThrows(ObjectInstanceNotKnown.class, () -> s.support.getUpdateRateValueForAttribute(Handles.objectInstance(999), Handles.attribute(51)));
            assertThrows(AttributeNotDefined.class, () -> s.support.getUpdateRateValueForAttribute(Handles.objectInstance(31), null));
            assertThrows(AttributeNotDefined.class, () -> s.support.getUpdateRateValueForAttribute(Handles.objectInstance(31), foreign(AttributeHandle.class)));
            assertTrue(s.requests.isEmpty());
        }
    }

    @Test void allFourteenServicesRequireConnectionAndMembership() throws Exception {
        try (Session s = new Session()) {
            for (RTIexception failure : List.of(new NotConnected("offline"), new FederateNotExecutionMember("resigned"))) {
                s.admissionFailure = failure;
                for (Executable service : s.all()) assertSame(failure, assertThrows(failure.getClass(), service));
            }
            assertTrue(s.requests.isEmpty());
        }
    }

    @Test void queryAndDirectiveAdmissionIsNotTheSaveRestoreMutationGate() throws Throwable {
        try (Session s = new Session()) {
            s.mutationFailure = new RestoreInProgress("restoring");
            for (Executable service : s.queries()) service.execute();
            for (Executable service : s.switches()) assertThrows(RestoreInProgress.class, service);
            assertEquals(6, s.requests.size());
        }
    }

    @Test void exactServerExceptionsAndUnavailableFailuresNeverBecomeSuccess() throws Exception {
        try (Session s = new Session()) {
            Class<?>[] duplicateTypes = {ObjectClassRelevanceAdvisorySwitchIsOn.class, ObjectClassRelevanceAdvisorySwitchIsOff.class,
                    AttributeRelevanceAdvisorySwitchIsOn.class, AttributeRelevanceAdvisorySwitchIsOff.class,
                    AttributeScopeAdvisorySwitchIsOn.class, AttributeScopeAdvisorySwitchIsOff.class,
                    InteractionRelevanceAdvisorySwitchIsOn.class, InteractionRelevanceAdvisorySwitchIsOff.class};
            for (int i = 0; i < duplicateTypes.length; i++) {
                s.failure = duplicateTypes[i].getSimpleName();
                assertEquals(duplicateTypes[i], assertThrows(RTIexception.class, s.switches().get(i)).getClass());
            }
            s.failure = "InvalidUpdateRateDesignator";
            assertThrows(InvalidUpdateRateDesignator.class, () -> s.support.getUpdateRateValue("missing"));
            s.failure = "ObjectInstanceNotKnown";
            assertThrows(ObjectInstanceNotKnown.class, () -> s.support.getUpdateRateValueForAttribute(Handles.objectInstance(31), Handles.attribute(51)));
            s.failure = "AttributeNotDefined";
            assertThrows(AttributeNotDefined.class, () -> s.support.getUpdateRateValueForAttribute(Handles.objectInstance(31), Handles.attribute(999)));
            s.failure = "InvalidFederateHandle";
            assertThrows(InvalidFederateHandle.class, () -> s.support.normalizeFederateHandle(Handles.federate(999)));
            s.failure = "InvalidServiceGroup";
            assertThrows(InvalidServiceGroup.class, () -> s.support.normalizeServiceGroup(ServiceGroup.SUPPORT_SERVICES));
            s.failure = "InvalidResignAction";
            assertThrows(InvalidResignAction.class, () -> s.support.setAutomaticResignDirective(ResignAction.NO_ACTION));
            for (String failure : List.of("SaveInProgress", "RestoreInProgress")) {
                s.failure = failure;
                for (Executable service : s.switches()) assertEquals(failure, assertThrows(RTIexception.class, service).getClass().getSimpleName());
            }
            s.failure = "UNAVAILABLE";
            for (Executable service : s.all()) assertThrows(RTIinternalError.class, service);
        }
    }

    @Test void missingSessionCredentialFailsAllServicesWithoutLocalSuccess() throws Exception {
        try (Session s = new Session()) {
            s.authenticated = false;
            for (Executable service : s.all()) assertThrows(RTIinternalError.class, service);
            assertTrue(s.requests.isEmpty());
        }
    }

    private static <T> T foreign(Class<T> type) {
        return type.cast(Proxy.newProxyInstance(type.getClassLoader(), new Class<?>[]{type},
                (proxy, method, args) -> { throw new AssertionError("Foreign handles must not be decoded"); }));
    }

    private static final class Session implements AutoCloseable, ServiceContext {
        static final Metadata.Key<byte[]> TOKEN = Metadata.Key.of("gorti-session-token-bin", Metadata.BINARY_BYTE_MARSHALLER);
        final List<Message> requests = new ArrayList<>();
        final Server server;
        final ManagedChannel transport;
        final Channel authenticatedChannel;
        final SupportServices support;
        boolean authenticated = true;
        RTIexception admissionFailure;
        RTIexception mutationFailure;
        String failure;
        double rate = 20;
        long normalized = 9;
        int action = 3;

        Session() throws Exception {
            String name = InProcessServerBuilder.generateName();
            ServerInterceptor auth = new ServerInterceptor() {
                public <Q, R> ServerCall.Listener<Q> interceptCall(ServerCall<Q, R> call, Metadata headers, ServerCallHandler<Q, R> next) {
                    if (!java.util.Arrays.equals(new byte[]{3, 1, 4}, headers.get(TOKEN))) {
                        call.close(Status.UNAUTHENTICATED, new Metadata());
                        return new ServerCall.Listener<Q>() { };
                    }
                    return next.startCall(call, headers);
                }
            };
            server = InProcessServerBuilder.forName(name).directExecutor().intercept(auth)
                    .addService(new SupportServiceGrpc.SupportServiceImplBase() {
                        @Override public void getUpdateRateValue(Support.GetUpdateRateValueRequest q, StreamObserver<Support.GetUpdateRateValueResponse> r) {
                            reply(q, r, Support.GetUpdateRateValueResponse.newBuilder().setUpdateRate(rate).build());
                        }
                        @Override public void getUpdateRateValueForAttribute(Support.GetUpdateRateValueForAttributeRequest q, StreamObserver<Support.GetUpdateRateValueForAttributeResponse> r) {
                            reply(q, r, Support.GetUpdateRateValueForAttributeResponse.newBuilder().setUpdateRate(rate).build());
                        }
                        @Override public void normalizeFederateHandle(Support.NormalizeFederateHandleRequest q, StreamObserver<Support.NormalizeFederateHandleResponse> r) {
                            reply(q, r, Support.NormalizeFederateHandleResponse.newBuilder().setValue(normalized).build());
                        }
                        @Override public void normalizeServiceGroup(Support.NormalizeServiceGroupRequest q, StreamObserver<Support.NormalizeServiceGroupResponse> r) {
                            reply(q, r, Support.NormalizeServiceGroupResponse.newBuilder().setValue(normalized).build());
                        }
                    }).addService(new FederationServiceGrpc.FederationServiceImplBase() {
                        @Override public void getAutomaticResignDirective(Federation.GetAutomaticResignDirectiveRequest q, StreamObserver<Federation.GetAutomaticResignDirectiveResponse> r) {
                            reply(q, r, Federation.GetAutomaticResignDirectiveResponse.newBuilder().setActionValue(action).build());
                        }
                        @Override public void setAutomaticResignDirective(Federation.SetAutomaticResignDirectiveRequest q, StreamObserver<Common.Empty> r) {
                            if (failure == null) action = q.getActionValue();
                            reply(q, r, Common.Empty.getDefaultInstance());
                        }
                    }).addService(new DeclarationServiceGrpc.DeclarationServiceImplBase() {
                        @Override public void setAdvisorySwitch(Declaration.SetAdvisorySwitchRequest q, StreamObserver<Common.Empty> r) {
                            reply(q, r, Common.Empty.getDefaultInstance());
                        }
                    }).build().start();
            transport = InProcessChannelBuilder.forName(name).directExecutor().build();
            Metadata credentials = new Metadata();
            credentials.put(TOKEN, new byte[]{3, 1, 4});
            authenticatedChannel = ClientInterceptors.intercept(transport, MetadataUtils.newAttachHeadersInterceptor(credentials));
            ExchangeServices exchange = new ExchangeServices(this);
            exchange.discovered(31, 21, "asset");
            support = new SupportServices(this, exchange);
        }

        <T> void reply(Message request, StreamObserver<T> response, T result) {
            assertEquals("fed", request.getField(request.getDescriptorForType().findFieldByName("federation_name")));
            assertEquals(7L, request.getField(request.getDescriptorForType().findFieldByName("federate_handle")));
            assertEquals(Common.WireVersion.WIRE_VERSION_V1.getValueDescriptor(), request.getField(request.getDescriptorForType().findFieldByName("wire_version")));
            requests.add(request);
            if (failure != null) {
                Metadata trailers = new Metadata();
                if (!failure.equals("UNAVAILABLE")) trailers.put(Metadata.Key.of("rti-spec-exception", Metadata.ASCII_STRING_MARSHALLER), failure);
                response.onError(Status.UNAVAILABLE.withDescription("test failure").asRuntimeException(trailers));
            } else {
                response.onNext(result);
                response.onCompleted();
            }
        }

        Message last() { return requests.get(requests.size() - 1); }
        List<Executable> switches() {
            return List.of(support::enableObjectClassRelevanceAdvisorySwitch, support::disableObjectClassRelevanceAdvisorySwitch,
                    support::enableAttributeRelevanceAdvisorySwitch, support::disableAttributeRelevanceAdvisorySwitch,
                    support::enableAttributeScopeAdvisorySwitch, support::disableAttributeScopeAdvisorySwitch,
                    support::enableInteractionRelevanceAdvisorySwitch, support::disableInteractionRelevanceAdvisorySwitch);
        }
        List<Executable> queries() {
            return List.of(() -> support.getUpdateRateValue("fast"),
                    () -> support.getUpdateRateValueForAttribute(Handles.objectInstance(31), Handles.attribute(51)),
                    support::getAutomaticResignDirective, () -> support.setAutomaticResignDirective(ResignAction.NO_ACTION),
                    () -> support.normalizeFederateHandle(Handles.federate(8)), () -> support.normalizeServiceGroup(ServiceGroup.SUPPORT_SERVICES));
        }
        List<Executable> all() { List<Executable> all = new ArrayList<>(queries()); all.addAll(switches()); return all; }
        public void requireMember() throws RTIexception { requireRestoreControl(); if (mutationFailure != null) throw mutationFailure; }
        public void requireRestoreControl() throws RTIexception { if (admissionFailure != null) throw admissionFailure; }
        public Channel channel() { return authenticated ? authenticatedChannel : transport; }
        public String federation() { return "fed"; }
        public long federate() { return 7; }
        public long generation() { return 9; }
        public boolean regulating() { return false; }
        public double logicalTime() { return 0; }
        public void close() throws InterruptedException {
            transport.shutdownNow(); server.shutdownNow();
            assertTrue(transport.awaitTermination(5, TimeUnit.SECONDS));
            assertTrue(server.awaitTermination(5, TimeUnit.SECONDS));
        }
    }
}
