package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;
import hla.rti1516e.FederateHandleSet;
import hla.rti1516e.exceptions.RTIinternalError;
import hla.rti1516e.exceptions.SynchronizationPointLabelNotAnnounced;
import io.grpc.Channel;
import io.grpc.ManagedChannel;
import io.grpc.Server;
import io.grpc.inprocess.InProcessChannelBuilder;
import io.grpc.inprocess.InProcessServerBuilder;
import io.grpc.stub.StreamObserver;
import java.util.concurrent.atomic.AtomicReference;
import org.gorti.rti.types.HandleCollections;
import org.gorti.rti.types.Handles;
import org.junit.jupiter.api.Test;
import rti.v1.Common;
import rti.v1.Sync;
import rti.v1.SyncServiceGrpc;

class SynchronizationServicesTest {
    @Test void preservesExplicitParticipantsTagAndFailedAchievement() throws Exception {
        try (Fixture fixture = new Fixture()) {
            FederateHandleSet participants = HandleCollections.federateSetFactory().create();
            participants.add(Handles.federate(7)); participants.add(Handles.federate(8));
            byte[] tag = {0, 3, -1};
            fixture.services.registerFederationSynchronizationPoint("ready", tag, participants);
            tag[1] = 99;
            assertEquals("demo", fixture.registration.get().getFederationName());
            assertEquals(7, fixture.registration.get().getFederateHandle());
            assertArrayEquals(new byte[]{0, 3, -1}, fixture.registration.get().getTag().toByteArray());
            assertEquals(2, fixture.registration.get().getRequiredFederatesCount());
            assertThrows(SynchronizationPointLabelNotAnnounced.class,
                    () -> fixture.services.synchronizationPointAchieved("ready"));
            assertNull(fixture.achievement.get());
            fixture.services.announced("ready");
            fixture.services.synchronizationPointAchieved("ready", false);
            assertTrue(fixture.achievement.get().hasSuccessfully());
            assertFalse(fixture.achievement.get().getSuccessfully());
            assertThrows(SynchronizationPointLabelNotAnnounced.class,
                    () -> fixture.services.synchronizationPointAchieved("ready"));
        }
    }

    @Test void explicitEmptySetUsesAllMemberRegistration() throws Exception {
        try (Fixture fixture = new Fixture()) {
            byte[] tag = {0, 3, -1};
            fixture.services.registerFederationSynchronizationPoint("implicit", tag);
            Sync.RegisterSyncPointRequest implicit = fixture.registration.get();
            assertEquals(0, implicit.getRequiredFederatesCount());
            fixture.registration.set(null);
            fixture.services.registerFederationSynchronizationPoint(
                    "explicit", tag, HandleCollections.federateSetFactory().create());
            Sync.RegisterSyncPointRequest explicit = fixture.registration.get();
            assertNotNull(explicit);
            assertEquals(implicit.toBuilder().setLabel("explicit").build(), explicit);
            tag[1] = 99;
            assertArrayEquals(new byte[]{0, 3, -1}, explicit.getTag().toByteArray());
            assertThrows(SynchronizationPointLabelNotAnnounced.class,
                    () -> fixture.services.synchronizationPointAchieved("explicit"));
            assertNull(fixture.achievement.get());

            fixture.services.registerFederationSynchronizationPoint(
                    "empty-tag", null, HandleCollections.federateSetFactory().create());
            assertEquals(0, fixture.registration.get().getRequiredFederatesCount());
            assertTrue(fixture.registration.get().getTag().isEmpty());
        }
    }

    @Test void explicitNullSetRemainsRejectedBeforeRegistration() throws Exception {
        try (Fixture fixture = new Fixture()) {
            assertThrows(RTIinternalError.class, () -> fixture.services.registerFederationSynchronizationPoint(
                    "null-set", new byte[0], null));
            assertNull(fixture.registration.get());
        }
    }

    @Test void clearDropsAnnouncementKnowledgeBetweenJoins() throws Exception {
        try (Fixture fixture = new Fixture()) {
            fixture.services.announced("ready");
            fixture.services.clear();
            assertThrows(SynchronizationPointLabelNotAnnounced.class,
                    () -> fixture.services.synchronizationPointAchieved("ready"));
        }
    }

    private static final class Fixture implements AutoCloseable, ServiceContext {
        final AtomicReference<Sync.RegisterSyncPointRequest> registration = new AtomicReference<>();
        final AtomicReference<Sync.AchieveSyncPointRequest> achievement = new AtomicReference<>();
        final Server server;
        final ManagedChannel channel;
        final SynchronizationServices services;
        Fixture() throws Exception {
            String name = InProcessServerBuilder.generateName();
            server = InProcessServerBuilder.forName(name).directExecutor().addService(new SyncServiceGrpc.SyncServiceImplBase() {
                @Override public void registerFederationSynchronizationPoint(Sync.RegisterSyncPointRequest request, StreamObserver<Common.Empty> reply) {
                    registration.set(request); reply.onNext(Common.Empty.getDefaultInstance()); reply.onCompleted();
                }
                @Override public void synchronizationPointAchieved(Sync.AchieveSyncPointRequest request, StreamObserver<Common.Empty> reply) {
                    achievement.set(request); reply.onNext(Common.Empty.getDefaultInstance()); reply.onCompleted();
                }
            }).build().start();
            channel = InProcessChannelBuilder.forName(name).directExecutor().build();
            services = new SynchronizationServices(this);
        }
        @Override public void close() { channel.shutdownNow(); server.shutdownNow(); }
        @Override public void requireMember() { }
        @Override public Channel channel() { return channel; }
        @Override public String federation() { return "demo"; }
        @Override public long federate() { return 7; }
        @Override public long generation() { return 4; }
        @Override public boolean regulating() { return false; }
        @Override public double logicalTime() { return 0; }
    }
}
