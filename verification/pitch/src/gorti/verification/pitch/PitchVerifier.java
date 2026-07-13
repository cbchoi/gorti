package gorti.verification.pitch;

import hla.rti1516e.AttributeHandle;
import hla.rti1516e.AttributeHandleSet;
import hla.rti1516e.AttributeHandleValueMap;
import hla.rti1516e.CallbackModel;
import hla.rti1516e.FederateHandle;
import hla.rti1516e.FederateHandleSet;
import hla.rti1516e.FederateAmbassador.SupplementalReceiveInfo;
import hla.rti1516e.FederateAmbassador.SupplementalReflectInfo;
import hla.rti1516e.FederateAmbassador.SupplementalRemoveInfo;
import hla.rti1516e.InteractionClassHandle;
import hla.rti1516e.LogicalTime;
import hla.rti1516e.MessageRetractionHandle;
import hla.rti1516e.NullFederateAmbassador;
import hla.rti1516e.ObjectClassHandle;
import hla.rti1516e.ObjectInstanceHandle;
import hla.rti1516e.OrderType;
import hla.rti1516e.ParameterHandle;
import hla.rti1516e.ParameterHandleValueMap;
import hla.rti1516e.RTIambassador;
import hla.rti1516e.ResignAction;
import hla.rti1516e.RtiFactory;
import hla.rti1516e.RtiFactoryFactory;
import hla.rti1516e.SynchronizationPointFailureReason;
import hla.rti1516e.TransportationTypeHandle;
import hla.rti1516e.encoding.DecoderException;
import hla.rti1516e.encoding.EncoderFactory;
import hla.rti1516e.encoding.HLAASCIIstring;
import hla.rti1516e.encoding.HLAinteger32BE;
import hla.rti1516e.exceptions.FederateInternalError;
import hla.rti1516e.exceptions.FederatesCurrentlyJoined;
import hla.rti1516e.exceptions.FederationExecutionAlreadyExists;
import hla.rti1516e.exceptions.FederationExecutionDoesNotExist;
import hla.rti1516e.exceptions.NameNotFound;
import hla.rti1516e.time.HLAfloat64Time;
import hla.rti1516e.time.HLAfloat64TimeFactory;

import java.io.BufferedWriter;
import java.io.Closeable;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.Arrays;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.TreeMap;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;
import java.util.function.BooleanSupplier;

/** Noninteractive two-federate verifier for the IEEE 1516-2010 Pitch Java API. */
@SuppressWarnings("rawtypes")
public final class PitchVerifier extends NullFederateAmbassador {
    private static final String OBJECT_CLASS = "VerifierEntity";
    private static final String INTERACTION_CLASS = "VerifierMessage";
    private static final String OBJECT_INSTANCE_NAME = "PitchVerifierEntity";
    private static final String READY_SYNC = "VERIFY_READY";
    private static final String DONE_SYNC = "VERIFY_DONE";
    private static final String PUBLISHER_NAME = "PitchVerifierPublisher";
    private static final String SUBSCRIBER_NAME = "PitchVerifierSubscriber";

    private final Config config;
    private final SemanticLogger semantic;
    private final MetricLogger metrics;
    private final SampleLogger samples;
    private final Map<Integer, Observation> reflected = new ConcurrentHashMap<Integer, Observation>();
    private final Map<Integer, Observation> received = new ConcurrentHashMap<Integer, Observation>();
    private final Map<Integer, GrantEvidence> grantEvidence =
            new ConcurrentHashMap<Integer, GrantEvidence>();
    private final AtomicInteger duplicateCallbacks = new AtomicInteger();
    private final AtomicInteger invalidCallbacks = new AtomicInteger();
    private final AtomicReference<Throwable> callbackFailure = new AtomicReference<Throwable>();

    private RTIambassador rti;
    private EncoderFactory encoders;
    private HLAfloat64TimeFactory timeFactory;
    private ObjectClassHandle objectClass;
    private AttributeHandle objectSequence;
    private AttributeHandle objectPayload;
    private InteractionClassHandle interactionClass;
    private ParameterHandle interactionSequence;
    private ParameterHandle interactionPayload;
    private ObjectInstanceHandle objectInstance;
    private FederateHandle selfHandle;

    private volatile boolean joined;
    private volatile boolean connected;
    private volatile boolean regulationEnabled;
    private volatile boolean constrainedEnabled;
    private volatile boolean timeAdvanceGranted;
    private volatile long grantedTime;
    private volatile int activeDeliveryBatch = -1;
    private volatile boolean objectDiscovered;
    private volatile ObjectInstanceHandle discoveredObject;
    private volatile String discoveredObjectName;
    private volatile boolean objectRemoved;
    private volatile long removedTime;
    private volatile boolean nameReservationComplete;
    private volatile boolean nameReservationSucceeded;
    private volatile boolean readyAnnounced;
    private volatile boolean readySynchronized;
    private volatile boolean doneAnnounced;
    private volatile boolean doneSynchronized;
    private volatile String synchronizationRegistrationSucceeded;
    private volatile String synchronizationFailure;

    public static void main(String[] args) {
        Config config = null;
        try {
            config = Config.parse(args);
            Files.createDirectories(config.outputDirectory);
            try (SemanticLogger semantic = new SemanticLogger(
                    config.outputDirectory.resolve(config.role + "-semantic.ndjson"), config.role);
                 MetricLogger metrics = new MetricLogger(
                    config.outputDirectory.resolve(config.role + "-metrics.ndjson"));
                 SampleLogger samples = new SampleLogger(
                    config.outputDirectory.resolve(config.role + "-samples.ndjson"))) {
                new PitchVerifier(config, semantic, metrics, samples).execute();
            }
        } catch (Throwable failure) {
            System.err.println("Pitch verifier failed: " + failure.getClass().getSimpleName()
                    + ": " + String.valueOf(failure.getMessage()));
            System.exit(1);
        }
    }

    private PitchVerifier(Config config, SemanticLogger semantic, MetricLogger metrics,
            SampleLogger samples) {
        this.config = config;
        this.semantic = semantic;
        this.metrics = metrics;
        this.samples = samples;
    }

    private void execute() throws Exception {
        long runStarted = System.nanoTime();
        boolean passed = false;
        semantic.event("FM", "phase", data(
                "count", config.count,
                "phase", "plan",
                "seed", config.seed,
                "status", "complete"));
        semantic.event("FM", "phase", data("phase", "do", "status", "start"));

        try {
            connectCreateAndJoin();
            resolveHandlesAndDeclareInterests();
            enableTimeManagement();
            synchronize(READY_SYNC);

            if (config.isPublisher()) {
                publishTraffic();
            } else {
                consumeTraffic();
            }

            synchronize(DONE_SYNC);
            semantic.event("FM", "phase", data("phase", "do", "status", "complete"));
            semantic.event("FM", "phase", data("phase", "review", "status", "start"));
            review();
            semantic.event("FM", "phase", data(
                    "count", config.count,
                    "phase", "review",
                    "status", "complete"));
            orderlyShutdown();
            passed = true;
            semantic.event("FM", "phase", data(
                    "phase", "reflect",
                    "result", "pass",
                    "status", "complete"));
        } catch (Throwable failure) {
            semantic.event("FM", "phase", data(
                    "error", failure.getClass().getSimpleName(),
                    "phase", "reflect",
                    "result", "fail",
                    "status", "complete"));
            throw failure;
        } finally {
            metrics.metric("FM", "run_duration", "nanoseconds", System.nanoTime() - runStarted);
            metrics.metric("FM", "verification_result", "boolean", passed ? 1 : 0);
            if (!passed) {
                bestEffortShutdown();
            }
        }
    }

    private void connectCreateAndJoin() throws Exception {
        RtiFactory factory = RtiFactoryFactory.getRtiFactory();
        rti = factory.getRtiAmbassador();
        encoders = factory.getEncoderFactory();

        timed("FM", "connect", new CheckedRunnable() {
            public void run() throws Exception {
                rti.connect(PitchVerifier.this, CallbackModel.HLA_IMMEDIATE,
                        "crcAddress=" + config.crcAddress);
            }
        });
        connected = true;
        semantic.event("FM", "connected", data("callback_model", "HLA_IMMEDIATE"));

        timed("FM", "create_federation_execution", new CheckedRunnable() {
            public void run() throws Exception {
                try {
                    rti.createFederationExecution(config.federationName,
                            new java.net.URL[]{config.fom.toUri().toURL()}, "HLAfloat64Time");
                } catch (FederationExecutionAlreadyExists expected) {
                    // Both processes may race to create the same federation.
                }
            }
        });
        semantic.event("FM", "federation_ready", data("fom", "Verification.xml"));

        final String federateName = config.isPublisher() ? PUBLISHER_NAME : SUBSCRIBER_NAME;
        timed("FM", "join_federation_execution", new CheckedRunnable() {
            public void run() throws Exception {
                selfHandle = rti.joinFederationExecution(federateName, "PitchVerifier-" + config.role,
                        config.federationName);
            }
        });
        joined = true;
        semantic.event("FM", "joined", data("federate_type", "PitchVerifier-" + config.role));
        timeFactory = (HLAfloat64TimeFactory) rti.getTimeFactory();
    }

    private void resolveHandlesAndDeclareInterests() throws Exception {
        objectClass = rti.getObjectClassHandle(OBJECT_CLASS);
        objectSequence = rti.getAttributeHandle(objectClass, "Sequence");
        objectPayload = rti.getAttributeHandle(objectClass, "Payload");
        interactionClass = rti.getInteractionClassHandle(INTERACTION_CLASS);
        interactionSequence = rti.getParameterHandle(interactionClass, "Sequence");
        interactionPayload = rti.getParameterHandle(interactionClass, "Payload");

        final AttributeHandleSet attributes = rti.getAttributeHandleSetFactory().create();
        attributes.add(objectSequence);
        attributes.add(objectPayload);
        if (config.isPublisher()) {
            timed("DM", "publish_object_class_attributes", new CheckedRunnable() {
                public void run() throws Exception {
                    rti.publishObjectClassAttributes(objectClass, attributes);
                }
            });
            semantic.event("DM", "object_published", data("class", OBJECT_CLASS));
            timed("DM", "publish_interaction_class", new CheckedRunnable() {
                public void run() throws Exception {
                    rti.publishInteractionClass(interactionClass);
                }
            });
            semantic.event("DM", "interaction_published", data("class", INTERACTION_CLASS));
        } else {
            timed("DM", "subscribe_object_class_attributes", new CheckedRunnable() {
                public void run() throws Exception {
                    rti.subscribeObjectClassAttributes(objectClass, attributes);
                }
            });
            semantic.event("DM", "object_subscribed", data("class", OBJECT_CLASS));
            timed("DM", "subscribe_interaction_class", new CheckedRunnable() {
                public void run() throws Exception {
                    rti.subscribeInteractionClass(interactionClass);
                }
            });
            semantic.event("DM", "interaction_subscribed", data("class", INTERACTION_CLASS));
        }
    }

    private void enableTimeManagement() throws Exception {
        timed("TM", "enable_time_regulation", new CheckedRunnable() {
            public void run() throws Exception {
                rti.enableTimeRegulation(timeFactory.makeInterval(1.0));
            }
        });
        await("time regulation enabled", new BooleanSupplier() {
            public boolean getAsBoolean() {
                return regulationEnabled;
            }
        });
        semantic.event("TM", "time_regulation_enabled", data("lookahead", 1));

        timed("TM", "enable_time_constrained", new CheckedRunnable() {
            public void run() throws Exception {
                rti.enableTimeConstrained();
            }
        });
        await("time constrained enabled", new BooleanSupplier() {
            public boolean getAsBoolean() {
                return constrainedEnabled;
            }
        });
        semantic.event("TM", "time_constrained_enabled", Collections.<String, Object>emptyMap());
    }

    private void synchronize(final String label) throws Exception {
        final boolean ready = READY_SYNC.equals(label);
        final boolean registrar = (ready && !config.isPublisher())
                || (!ready && config.isPublisher());
        if (registrar) {
            final String peerName = config.isPublisher() ? SUBSCRIBER_NAME : PUBLISHER_NAME;
            final String peerActor = config.isPublisher() ? "subscriber" : "publisher";
            final FederateHandle peerHandle = awaitFederatePresent(peerName);
            final FederateHandleSet participants = rti.getFederateHandleSetFactory().create();
            participants.add(selfHandle);
            participants.add(peerHandle);
            synchronizationRegistrationSucceeded = null;
            synchronizationFailure = null;
            timed("FM", "register_synchronization_point", new CheckedRunnable() {
                public void run() throws Exception {
                    rti.registerFederationSynchronizationPoint(label, null, participants);
                }
            });
            await("synchronization registration result: " + label, new BooleanSupplier() {
                public boolean getAsBoolean() {
                    return label.equals(synchronizationRegistrationSucceeded)
                            || synchronizationFailure != null;
                }
            });
            if (synchronizationFailure != null) {
                throw new IllegalStateException(synchronizationFailure);
            }
            semantic.event("FM", "peer_joined", data("peer", peerActor));
            semantic.event("FM", "synchronization_registered", data(
                    "label", label,
                    "participants", 2));
        }

        await("synchronization point announced: " + label, new BooleanSupplier() {
            public boolean getAsBoolean() {
                return (ready ? readyAnnounced : doneAnnounced)
                        || synchronizationFailure != null;
            }
        });
        if (synchronizationFailure != null) {
            throw new IllegalStateException(synchronizationFailure);
        }
        semantic.event("FM", "synchronization_announced", data("label", label));

        timed("FM", "synchronization_point_achieved", new CheckedRunnable() {
            public void run() throws Exception {
                rti.synchronizationPointAchieved(label);
            }
        });
        semantic.event("FM", "synchronization_achieved", data("label", label));
        await("federation synchronized: " + label, new BooleanSupplier() {
            public boolean getAsBoolean() {
                return ready ? readySynchronized : doneSynchronized;
            }
        });
        semantic.event("FM", "federation_synchronized", data("label", label));
    }

    private void publishTraffic() throws Exception {
        nameReservationComplete = false;
        nameReservationSucceeded = false;
        timed("OM", "reserve_object_instance_name", new CheckedRunnable() {
            public void run() throws Exception {
                rti.reserveObjectInstanceName(OBJECT_INSTANCE_NAME);
            }
        });
        await("object instance name reservation", new BooleanSupplier() {
            public boolean getAsBoolean() {
                return nameReservationComplete;
            }
        });
        if (!nameReservationSucceeded) {
            throw new IllegalStateException("object instance name reservation failed");
        }
        semantic.event("OM", "object_name_reserved", data("name", OBJECT_INSTANCE_NAME));

        timed("OM", "register_object_instance", new CheckedRunnable() {
            public void run() throws Exception {
                objectInstance = rti.registerObjectInstance(objectClass, OBJECT_INSTANCE_NAME);
            }
        });
        semantic.event("OM", "object_registered", data(
                "class", OBJECT_CLASS,
                "name", OBJECT_INSTANCE_NAME));

        for (int index = 0; index < config.count; index++) {
            final int item = index;
            final long logicalTime = index + 1L;
            final HLAfloat64Time timestamp = timeFactory.makeTime((double) logicalTime);
            final String objectValue = deterministicPayload(config.seed, "attribute", index);
            final String interactionValue = deterministicPayload(config.seed, "interaction", index);

            final AttributeHandleValueMap attributes = rti.getAttributeHandleValueMapFactory().create(2);
            attributes.put(objectSequence, encodeInteger(item));
            attributes.put(objectPayload, encodeString(objectValue));
            benchmarkTimed("OM", "updateAttributeValues", "update_attribute_values",
                    new CheckedRunnable() {
                public void run() throws Exception {
                    rti.updateAttributeValues(objectInstance, attributes, null, timestamp);
                }
            });
            semantic.event("OM", "attributes_updated", data(
                    "index", item,
                    "logical_time", logicalTime,
                    "payload", objectValue));

            final ParameterHandleValueMap parameters = rti.getParameterHandleValueMapFactory().create(2);
            parameters.put(interactionSequence, encodeInteger(item));
            parameters.put(interactionPayload, encodeString(interactionValue));
            benchmarkTimed("OM", "sendInteraction", "send_interaction",
                    new CheckedRunnable() {
                public void run() throws Exception {
                    rti.sendInteraction(interactionClass, parameters, null, timestamp);
                }
            });
            semantic.event("OM", "interaction_sent", data(
                    "index", item,
                    "logical_time", logicalTime,
                    "payload", interactionValue));
            advanceTo(logicalTime, true);
        }

        final long removalTime = config.count + 1L;
        timed("OM", "delete_object_instance", new CheckedRunnable() {
            public void run() throws Exception {
                rti.deleteObjectInstance(objectInstance, null,
                        timeFactory.makeTime((double) removalTime));
            }
        });
        semantic.event("OM", "object_deleted", data(
                "logical_time", removalTime,
                "name", OBJECT_INSTANCE_NAME));
        advanceTo(removalTime, false);
    }

    private void consumeTraffic() throws Exception {
        long sustainedStarted = -1L;
        long lastDeliveryCompleted = -1L;
        for (int index = 0; index < config.count; index++) {
            final int item = index;
            final long logicalTime = index + 1L;
            long batchStarted = advanceTo(logicalTime, true);
            if (sustainedStarted < 0L) {
                sustainedStarted = batchStarted;
            }
            await("object update " + item, new BooleanSupplier() {
                public boolean getAsBoolean() {
                    return reflected.containsKey(item);
                }
            });
            await("interaction " + item, new BooleanSupplier() {
                public boolean getAsBoolean() {
                    return received.containsKey(item);
                }
            });
            if (index == 0) {
                await("object discovery", new BooleanSupplier() {
                    public boolean getAsBoolean() {
                        return objectDiscovered;
                    }
                });
                if (!OBJECT_INSTANCE_NAME.equals(discoveredObjectName)) {
                    throw new IllegalStateException("unexpected object instance name");
                }
                semantic.event("OM", "object_discovered", data(
                        "class", OBJECT_CLASS,
                        "name", OBJECT_INSTANCE_NAME));
            }

            Observation objectObservation = reflected.get(item);
            Observation interactionObservation = received.get(item);
            long batchCompleted = Math.max(objectObservation.completedAtNanos,
                    interactionObservation.completedAtNanos);
            lastDeliveryCompleted = batchCompleted;
            assertObservation("attribute", item, logicalTime, objectObservation);
            assertObservation("interaction", item, logicalTime, interactionObservation);
            semantic.event("OM", "attributes_reflected", data(
                    "index", item,
                    "logical_time", logicalTime,
                    "payload", objectObservation.payload));
            semantic.event("OM", "interaction_received", data(
                    "index", item,
                    "logical_time", logicalTime,
                    "payload", interactionObservation.payload));
            metrics.metric("OM", "completed_delivery_batch_latency", "nanoseconds",
                    batchCompleted - batchStarted);
            samples.sample("completed_delivery_batch_latency", batchCompleted - batchStarted,
                    "OM", "delivery");
        }
        long sustainedDuration = Math.max(1L, lastDeliveryCompleted - sustainedStarted);
        metrics.metric("OM", "sustained_throughput", "deliveries_per_second",
                (config.count * 2.0d * 1_000_000_000.0d) / sustainedDuration);

        final long removalTime = config.count + 1L;
        advanceTo(removalTime, false);
        await("object removal", new BooleanSupplier() {
            public boolean getAsBoolean() {
                return objectRemoved;
            }
        });
        if (removedTime != removalTime) {
            throw new IllegalStateException("object removal did not preserve TSO logical time");
        }
        semantic.event("OM", "object_removed", data(
                "logical_time", removalTime,
                "name", OBJECT_INSTANCE_NAME));
    }

    private long advanceTo(final long logicalTime, boolean benchmarkSample) throws Exception {
        HLAfloat64Time requestedTime = timeFactory.makeTime((double) logicalTime);
        final int deliveryBatch = !config.isPublisher() && benchmarkSample
                ? Math.toIntExact(logicalTime - 1L) : -1;
        activeDeliveryBatch = deliveryBatch;
        timeAdvanceGranted = false;
        long started = System.nanoTime();
        try {
            rti.timeAdvanceRequest(requestedTime);
            long elapsed = System.nanoTime() - started;
            metrics.metric("TM", "call_latency.time_advance_request", "nanoseconds", elapsed);
            if (benchmarkSample) {
                samples.sample("timeAdvanceRequest", elapsed, "TM", "call");
            }
            semantic.event("TM", "time_advance_requested", data("logical_time", logicalTime));
            await("time advance grant " + logicalTime, new BooleanSupplier() {
                public boolean getAsBoolean() {
                    return timeAdvanceGranted && grantedTime == logicalTime;
                }
            });
            semantic.event("TM", "time_advance_granted", data("logical_time", logicalTime));
            return started;
        } finally {
            if (deliveryBatch >= 0) {
                activeDeliveryBatch = -1;
                GrantEvidence evidence = grantEvidence.get(deliveryBatch);
                if (evidence != null) {
                    semantic.event("TM", "callback_before_grant_guard", evidence.asData());
                }
            }
        }
    }

    private void review() {
        checkCallbackFailure();
        if (config.isPublisher()) {
            if (objectInstance == null) {
                throw new IllegalStateException("publisher did not register its object");
            }
        } else {
            if (!objectDiscovered || reflected.size() != config.count || received.size() != config.count
                    || grantEvidence.size() != config.count || duplicateCallbacks.get() != 0
                    || invalidCallbacks.get() != 0) {
                throw new IllegalStateException("subscriber callback totals did not match count");
            }
            metrics.metric("OM", "attributes_reflected", "count", reflected.size());
            metrics.metric("OM", "interactions_received", "count", received.size());
            metrics.metric("OM", "delivery_accounting.duplicates", "count",
                    duplicateCallbacks.get());
            metrics.metric("OM", "delivery_accounting.invalid", "count",
                    invalidCallbacks.get());
            metrics.metric("TM", "callback_before_grant_guard.passed", "count",
                    grantEvidence.size());
        }
        if (grantedTime != config.count + 1L) {
            throw new IllegalStateException("final logical time did not match count");
        }
        metrics.metric("TM", "final_logical_time", "ticks", grantedTime);
    }

    private void orderlyShutdown() throws Exception {
        if (config.isPublisher()) {
            awaitFederateAbsent(SUBSCRIBER_NAME);
            semantic.event("FM", "peer_resigned", data("peer", "subscriber"));
        }

        timed("FM", "resign_federation_execution", new CheckedRunnable() {
            public void run() throws Exception {
                rti.resignFederationExecution(ResignAction.DELETE_OBJECTS_THEN_DIVEST);
            }
        });
        joined = false;
        semantic.event("FM", "resigned", Collections.<String, Object>emptyMap());

        if (config.isPublisher()) {
            timed("FM", "destroy_federation_execution", new CheckedRunnable() {
                public void run() throws Exception {
                    rti.destroyFederationExecution(config.federationName);
                }
            });
            semantic.event("FM", "federation_destroyed", Collections.<String, Object>emptyMap());
        }

        timed("FM", "disconnect", new CheckedRunnable() {
            public void run() throws Exception {
                rti.disconnect();
            }
        });
        connected = false;
        semantic.event("FM", "disconnected", Collections.<String, Object>emptyMap());
    }

    private void bestEffortShutdown() {
        if (rti == null) {
            return;
        }
        if (joined) {
            try {
                rti.resignFederationExecution(ResignAction.DELETE_OBJECTS_THEN_DIVEST);
                joined = false;
            } catch (Throwable ignored) {
                // Preserve the original verification failure.
            }
        }
        if (config.isPublisher() && connected) {
            try {
                rti.destroyFederationExecution(config.federationName);
            } catch (FederatesCurrentlyJoined ignored) {
            } catch (FederationExecutionDoesNotExist ignored) {
            } catch (Throwable ignored) {
            }
        }
        if (connected) {
            try {
                rti.disconnect();
            } catch (Throwable ignored) {
            }
        }
    }

    private FederateHandle awaitFederatePresent(String federateName) throws Exception {
        long deadline = System.nanoTime() + config.timeoutMillis * 1_000_000L;
        while (true) {
            checkCallbackFailure();
            try {
                return rti.getFederateHandle(federateName);
            } catch (NameNotFound expected) {
                if (System.nanoTime() >= deadline) {
                    throw new IllegalStateException("timed out waiting for federate present: "
                            + federateName);
                }
                Thread.sleep(10L);
            }
        }
    }

    private void awaitFederateAbsent(final String federateName) throws Exception {
        await("federate absent: " + federateName, new BooleanSupplier() {
            public boolean getAsBoolean() {
                try {
                    rti.getFederateHandle(federateName);
                    return false;
                } catch (NameNotFound expected) {
                    return true;
                } catch (Exception failure) {
                    callbackFailure.compareAndSet(null, failure);
                    return false;
                }
            }
        });
        checkCallbackFailure();
    }

    private void await(String description, BooleanSupplier condition) throws Exception {
        long deadline = System.nanoTime() + config.timeoutMillis * 1_000_000L;
        while (!condition.getAsBoolean()) {
            checkCallbackFailure();
            if (System.nanoTime() >= deadline) {
                throw new IllegalStateException("timed out waiting for " + description);
            }
            Thread.sleep(10L);
        }
        checkCallbackFailure();
    }

    private void assertObservation(String channel, int index, long logicalTime, Observation observation) {
        if (observation == null) {
            throw new IllegalStateException("missing " + channel + " observation " + index);
        }
        if (!observationMatches(channel, index, logicalTime, observation)) {
            throw new IllegalStateException("invalid " + channel + " observation " + index);
        }
    }

    private boolean observationMatches(String channel, int index, long logicalTime,
            Observation observation) {
        return observation != null
                && logicalTime == observation.logicalTime
                && deterministicPayload(config.seed, channel, index).equals(observation.payload);
    }

    private byte[] encodeInteger(int value) {
        HLAinteger32BE encoder = encoders.createHLAinteger32BE(value);
        return encoder.toByteArray();
    }

    private byte[] encodeString(String value) {
        HLAASCIIstring encoder = encoders.createHLAASCIIstring(value);
        return encoder.toByteArray();
    }

    private int decodeInteger(byte[] bytes) throws DecoderException {
        HLAinteger32BE decoder = encoders.createHLAinteger32BE();
        decoder.decode(bytes);
        return decoder.getValue();
    }

    private String decodeString(byte[] bytes) throws DecoderException {
        HLAASCIIstring decoder = encoders.createHLAASCIIstring();
        decoder.decode(bytes);
        return decoder.getValue();
    }

    private static long logicalTime(LogicalTime time) {
        return Math.round(((HLAfloat64Time) time).getValue());
    }

    private void timed(String service, String metric, CheckedRunnable operation) throws Exception {
        long started = System.nanoTime();
        operation.run();
        metrics.metric(service, "call_latency." + metric, "nanoseconds",
                System.nanoTime() - started);
    }

    private void benchmarkTimed(String service, String operationName, String metric,
            CheckedRunnable operation) throws Exception {
        long started = System.nanoTime();
        operation.run();
        long elapsed = System.nanoTime() - started;
        metrics.metric(service, "call_latency." + metric, "nanoseconds", elapsed);
        samples.sample(operationName, elapsed, service, "call");
    }

    private void checkCallbackFailure() {
        Throwable failure = callbackFailure.get();
        if (failure != null) {
            throw new IllegalStateException("callback failed", failure);
        }
    }

    public void timeRegulationEnabled(LogicalTime time) throws FederateInternalError {
        regulationEnabled = true;
    }

    public void timeConstrainedEnabled(LogicalTime time) throws FederateInternalError {
        constrainedEnabled = true;
    }

    public void timeAdvanceGrant(LogicalTime time) throws FederateInternalError {
        long callbackTime = logicalTime(time);
        long grantReceivedAtNanos = System.nanoTime();
        int deliveryBatch = activeDeliveryBatch;
        if (!config.isPublisher() && deliveryBatch >= 0) {
            Observation reflection = reflected.get(deliveryBatch);
            Observation interaction = received.get(deliveryBatch);
            long expectedLogicalTime = deliveryBatch + 1L;
            boolean reflectionAccepted = observationMatches(
                    "attribute", deliveryBatch, expectedLogicalTime, reflection);
            boolean interactionAccepted = observationMatches(
                    "interaction", deliveryBatch, expectedLogicalTime, interaction);
            boolean callbacksBeforeGrant = reflectionAccepted && interactionAccepted
                    && reflection.completedAtNanos <= grantReceivedAtNanos
                    && interaction.completedAtNanos <= grantReceivedAtNanos;
            GrantEvidence evidence = new GrantEvidence(
                    deliveryBatch,
                    callbackTime,
                    reflection == null ? null : Long.valueOf(reflection.completedAtNanos),
                    interaction == null ? null : Long.valueOf(interaction.completedAtNanos),
                    grantReceivedAtNanos,
                    reflectionAccepted,
                    interactionAccepted,
                    callbacksBeforeGrant);
            GrantEvidence previous = grantEvidence.putIfAbsent(deliveryBatch, evidence);
            if (previous != null) {
                callbackFailure.compareAndSet(null,
                        new IllegalStateException("duplicate time advance grant evidence for batch "
                                + deliveryBatch));
                return;
            }
            if (callbackTime != expectedLogicalTime) {
                callbackFailure.compareAndSet(null, new IllegalStateException(
                        "time advance grant " + callbackTime + " arrived for delivery batch "
                                + deliveryBatch + ", expected " + expectedLogicalTime));
                return;
            }
            if (!callbacksBeforeGrant) {
                callbackFailure.compareAndSet(null, new IllegalStateException(
                        "time advance grant " + callbackTime
                                + " arrived before delivery batch " + deliveryBatch
                                + " completed"));
                return;
            }
        }
        grantedTime = callbackTime;
        timeAdvanceGranted = true;
    }

    public void synchronizationPointRegistrationSucceeded(String label)
            throws FederateInternalError {
        synchronizationRegistrationSucceeded = label;
    }

    public void synchronizationPointRegistrationFailed(String label,
            SynchronizationPointFailureReason reason) throws FederateInternalError {
        synchronizationFailure = "synchronization registration failed for " + label + ": " + reason;
    }

    public void announceSynchronizationPoint(String label, byte[] tag) throws FederateInternalError {
        if (READY_SYNC.equals(label)) {
            readyAnnounced = true;
        } else if (DONE_SYNC.equals(label)) {
            doneAnnounced = true;
        }
    }

    public void federationSynchronized(String label, FederateHandleSet failed)
            throws FederateInternalError {
        if (READY_SYNC.equals(label)) {
            readySynchronized = true;
        } else if (DONE_SYNC.equals(label)) {
            doneSynchronized = true;
        }
    }

    public void discoverObjectInstance(ObjectInstanceHandle instance, ObjectClassHandle objectClass,
            String objectName) throws FederateInternalError {
        if (this.objectClass != null && this.objectClass.equals(objectClass)) {
            discoveredObject = instance;
            discoveredObjectName = objectName;
            objectDiscovered = true;
        }
    }

    public void objectInstanceNameReservationSucceeded(String objectName)
            throws FederateInternalError {
        if (OBJECT_INSTANCE_NAME.equals(objectName)) {
            nameReservationSucceeded = true;
            nameReservationComplete = true;
        }
    }

    public void objectInstanceNameReservationFailed(String objectName)
            throws FederateInternalError {
        if (OBJECT_INSTANCE_NAME.equals(objectName)) {
            nameReservationSucceeded = false;
            nameReservationComplete = true;
        }
    }

    public void removeObjectInstance(ObjectInstanceHandle instance, byte[] tag, OrderType sentOrder,
            LogicalTime time, OrderType receivedOrder, SupplementalRemoveInfo info)
            throws FederateInternalError {
        captureRemoval(instance, time);
    }

    public void removeObjectInstance(ObjectInstanceHandle instance, byte[] tag, OrderType sentOrder,
            LogicalTime time, OrderType receivedOrder, MessageRetractionHandle retraction,
            SupplementalRemoveInfo info) throws FederateInternalError {
        captureRemoval(instance, time);
    }

    public void removeObjectInstance(ObjectInstanceHandle instance, byte[] tag, OrderType sentOrder,
            SupplementalRemoveInfo info) throws FederateInternalError {
        callbackFailure.compareAndSet(null,
                new IllegalStateException("object removal arrived without TSO logical time"));
    }

    private void captureRemoval(ObjectInstanceHandle instance, LogicalTime time) {
        if (discoveredObject != null && discoveredObject.equals(instance)) {
            removedTime = logicalTime(time);
            objectRemoved = true;
        }
    }

    public void reflectAttributeValues(ObjectInstanceHandle instance, AttributeHandleValueMap attributes,
            byte[] tag, OrderType sentOrder, TransportationTypeHandle transport, LogicalTime time,
            OrderType receivedOrder, SupplementalReflectInfo info) throws FederateInternalError {
        captureReflection(attributes, time);
    }

    public void reflectAttributeValues(ObjectInstanceHandle instance, AttributeHandleValueMap attributes,
            byte[] tag, OrderType sentOrder, TransportationTypeHandle transport, LogicalTime time,
            OrderType receivedOrder, MessageRetractionHandle retraction, SupplementalReflectInfo info)
            throws FederateInternalError {
        captureReflection(attributes, time);
    }

    private void captureReflection(AttributeHandleValueMap attributes, LogicalTime time) {
        try {
            if (attributes.containsKey(objectSequence) && attributes.containsKey(objectPayload)) {
                int index = decodeInteger(attributes.get(objectSequence));
                if (index < 0 || index >= config.count) {
                    invalidCallbacks.incrementAndGet();
                    return;
                }
                Observation previous = reflected.putIfAbsent(index,
                        new Observation(decodeString(attributes.get(objectPayload)),
                                logicalTime(time), System.nanoTime()));
                if (previous != null) {
                    duplicateCallbacks.incrementAndGet();
                }
            } else {
                invalidCallbacks.incrementAndGet();
            }
        } catch (Throwable failure) {
            callbackFailure.compareAndSet(null, failure);
        }
    }

    public void receiveInteraction(InteractionClassHandle interaction, ParameterHandleValueMap parameters,
            byte[] tag, OrderType sentOrder, TransportationTypeHandle transport, LogicalTime time,
            OrderType receivedOrder, SupplementalReceiveInfo info) throws FederateInternalError {
        captureInteraction(interaction, parameters, time);
    }

    public void receiveInteraction(InteractionClassHandle interaction, ParameterHandleValueMap parameters,
            byte[] tag, OrderType sentOrder, TransportationTypeHandle transport, LogicalTime time,
            OrderType receivedOrder, MessageRetractionHandle retraction, SupplementalReceiveInfo info)
            throws FederateInternalError {
        captureInteraction(interaction, parameters, time);
    }

    private void captureInteraction(InteractionClassHandle interaction,
            ParameterHandleValueMap parameters, LogicalTime time) {
        try {
            if (interactionClass.equals(interaction)
                    && parameters.containsKey(interactionSequence)
                    && parameters.containsKey(interactionPayload)) {
                int index = decodeInteger(parameters.get(interactionSequence));
                if (index < 0 || index >= config.count) {
                    invalidCallbacks.incrementAndGet();
                    return;
                }
                Observation previous = received.putIfAbsent(index,
                        new Observation(decodeString(parameters.get(interactionPayload)),
                                logicalTime(time), System.nanoTime()));
                if (previous != null) {
                    duplicateCallbacks.incrementAndGet();
                }
            } else {
                invalidCallbacks.incrementAndGet();
            }
        } catch (Throwable failure) {
            callbackFailure.compareAndSet(null, failure);
        }
    }

    static String deterministicPayload(String seed, String channel, int index) {
        try {
            MessageDigest digest = MessageDigest.getInstance("SHA-256");
            byte[] hash = digest.digest((seed + ":" + channel + ":" + index)
                    .getBytes(StandardCharsets.UTF_8));
            StringBuilder hex = new StringBuilder(64);
            for (byte value : hash) {
                hex.append(String.format("%02x", value & 0xff));
            }
            return hex.substring(0, 16);
        } catch (NoSuchAlgorithmException impossible) {
            throw new IllegalStateException("SHA-256 is unavailable", impossible);
        }
    }

    private static Map<String, Object> data(Object... values) {
        if (values.length % 2 != 0) {
            throw new IllegalArgumentException("data requires key/value pairs");
        }
        Map<String, Object> result = new LinkedHashMap<String, Object>();
        for (int index = 0; index < values.length; index += 2) {
            result.put(String.valueOf(values[index]), values[index + 1]);
        }
        return result;
    }

    private interface CheckedRunnable {
        void run() throws Exception;
    }

    private static final class Observation {
        private final String payload;
        private final long logicalTime;
        private final long completedAtNanos;

        private Observation(String payload, long logicalTime, long completedAtNanos) {
            this.payload = payload;
            this.logicalTime = logicalTime;
            this.completedAtNanos = completedAtNanos;
        }
    }

    private static final class GrantEvidence {
        private final int batchIndex;
        private final long logicalTime;
        private final Long reflectionCompletedAtNanos;
        private final Long interactionCompletedAtNanos;
        private final long grantReceivedAtNanos;
        private final boolean reflectionAccepted;
        private final boolean interactionAccepted;
        private final boolean passed;

        private GrantEvidence(int batchIndex, long logicalTime,
                Long reflectionCompletedAtNanos, Long interactionCompletedAtNanos,
                long grantReceivedAtNanos, boolean reflectionAccepted,
                boolean interactionAccepted, boolean passed) {
            this.batchIndex = batchIndex;
            this.logicalTime = logicalTime;
            this.reflectionCompletedAtNanos = reflectionCompletedAtNanos;
            this.interactionCompletedAtNanos = interactionCompletedAtNanos;
            this.grantReceivedAtNanos = grantReceivedAtNanos;
            this.reflectionAccepted = reflectionAccepted;
            this.interactionAccepted = interactionAccepted;
            this.passed = passed;
        }

        private Map<String, Object> asData() {
            return data(
                    "batch_index", batchIndex,
                    "callbacks_completed_before_grant", passed,
                    "grant_received_at_nanos", grantReceivedAtNanos,
                    "interaction_accepted", interactionAccepted,
                    "interaction_completed_at_nanos", interactionCompletedAtNanos,
                    "logical_time", logicalTime,
                    "reflection_accepted", reflectionAccepted,
                    "reflection_completed_at_nanos", reflectionCompletedAtNanos,
                    "result", passed ? "pass" : "fail");
        }
    }

    private static final class Config {
        private final String role;
        private final String seed;
        private final int count;
        private final String crcAddress;
        private final String federationName;
        private final Path fom;
        private final Path outputDirectory;
        private final long timeoutMillis;

        private Config(Map<String, String> args) {
            role = required(args, "role").toLowerCase();
            if (!Arrays.asList("publisher", "subscriber").contains(role)) {
                throw new IllegalArgumentException("--role must be publisher or subscriber");
            }
            seed = value(args, "seed", "1516");
            count = Integer.parseInt(value(args, "count", "100"));
            crcAddress = value(args, "crc", "localhost:8989");
            federationName = value(args, "federation", "GortiPitchVerifier-" + seed);
            fom = Paths.get(required(args, "fom")).toAbsolutePath().normalize();
            outputDirectory = Paths.get(required(args, "output")).toAbsolutePath().normalize();
            timeoutMillis = Long.parseLong(value(args, "timeout-ms", "30000"));
            if (count <= 0 || timeoutMillis <= 0) {
                throw new IllegalArgumentException("--count and --timeout-ms must be positive");
            }
            if (!Files.isRegularFile(fom)) {
                throw new IllegalArgumentException("FOM does not exist: " + fom);
            }
        }

        private static Config parse(String[] commandLine) {
            if (commandLine.length % 2 != 0) {
                throw new IllegalArgumentException("arguments must be --name value pairs");
            }
            Map<String, String> values = new LinkedHashMap<String, String>();
            for (int index = 0; index < commandLine.length; index += 2) {
                String name = commandLine[index];
                if (!name.startsWith("--") || name.length() == 2) {
                    throw new IllegalArgumentException("invalid argument: " + name);
                }
                values.put(name.substring(2), commandLine[index + 1]);
            }
            return new Config(values);
        }

        private boolean isPublisher() {
            return "publisher".equals(role);
        }

        private static String required(Map<String, String> args, String key) {
            String result = args.get(key);
            if (result == null || result.isEmpty()) {
                throw new IllegalArgumentException("missing --" + key);
            }
            return result;
        }

        private static String value(Map<String, String> args, String key, String fallback) {
            String result = args.get(key);
            return result == null ? fallback : result;
        }
    }

    private static final class SemanticLogger implements Closeable {
        private final BufferedWriter writer;
        private final String actor;
        private long sequence;

        private SemanticLogger(Path path, String actor) throws IOException {
            this.writer = Files.newBufferedWriter(path, StandardCharsets.UTF_8);
            this.actor = actor;
        }

        private synchronized void event(String service, String event, Map<String, Object> data)
                throws IOException {
            validateService(service);
            writer.write("{\"kind\":\"semantic\",\"seq\":" + sequence++
                    + ",\"service\":" + Json.string(service)
                    + ",\"event\":" + Json.string(event)
                    + ",\"actor\":" + Json.string(actor)
                    + ",\"data\":" + Json.object(data) + "}");
            writer.newLine();
            writer.flush();
        }

        public void close() throws IOException {
            writer.close();
        }
    }

    private static final class MetricLogger implements Closeable {
        private final BufferedWriter writer;

        private MetricLogger(Path path) throws IOException {
            writer = Files.newBufferedWriter(path, StandardCharsets.UTF_8);
        }

        private synchronized void metric(String service, String metric, String unit, Number value) {
            try {
                validateService(service);
                writer.write("{\"kind\":\"metric\",\"service\":" + Json.string(service)
                        + ",\"metric\":" + Json.string(metric)
                        + ",\"unit\":" + Json.string(unit)
                        + ",\"value\":" + value.toString() + "}");
                writer.newLine();
                writer.flush();
            } catch (IOException failure) {
                throw new IllegalStateException("unable to write metric", failure);
            }
        }

        public void close() throws IOException {
            writer.close();
        }
    }

    private static final class SampleLogger implements Closeable {
        private final BufferedWriter writer;
        private long sequence;

        private SampleLogger(Path path) throws IOException {
            writer = Files.newBufferedWriter(path, StandardCharsets.UTF_8);
        }

        private synchronized void sample(String operation, long durationNanos, String service,
                String sampleKind) {
            if (durationNanos < 0L) {
                throw new IllegalArgumentException("sample duration must be non-negative");
            }
            try {
                validateService(service);
                writer.write("{\"sequence\":" + sequence++
                        + ",\"operation\":" + Json.string(operation)
                        + ",\"duration_ns\":" + durationNanos
                        + ",\"dimensions\":" + Json.object(data(
                                "sample_kind", sampleKind,
                                "service", service)) + "}");
                writer.newLine();
                writer.flush();
            } catch (IOException failure) {
                throw new IllegalStateException("unable to write raw sample", failure);
            }
        }

        public void close() throws IOException {
            writer.close();
        }
    }

    private static final class Json {
        private static String object(Map<String, Object> values) {
            StringBuilder result = new StringBuilder("{");
            boolean first = true;
            for (Map.Entry<String, Object> entry : new TreeMap<String, Object>(values).entrySet()) {
                if (!first) {
                    result.append(',');
                }
                first = false;
                result.append(string(entry.getKey())).append(':').append(value(entry.getValue()));
            }
            return result.append('}').toString();
        }

        private static String value(Object value) {
            if (value == null) {
                return "null";
            }
            if (value instanceof Number || value instanceof Boolean) {
                return value.toString();
            }
            if (value instanceof Map) {
                @SuppressWarnings("unchecked")
                Map<String, Object> map = (Map<String, Object>) value;
                return object(map);
            }
            return string(String.valueOf(value));
        }

        private static String string(String value) {
            StringBuilder result = new StringBuilder("\"");
            for (int index = 0; index < value.length(); index++) {
                char character = value.charAt(index);
                switch (character) {
                    case '\\': result.append("\\\\"); break;
                    case '"': result.append("\\\""); break;
                    case '\b': result.append("\\b"); break;
                    case '\f': result.append("\\f"); break;
                    case '\n': result.append("\\n"); break;
                    case '\r': result.append("\\r"); break;
                    case '\t': result.append("\\t"); break;
                    default:
                        if (character < 0x20) {
                            result.append(String.format("\\u%04x", (int) character));
                        } else {
                            result.append(character);
                        }
                }
            }
            return result.append('"').toString();
        }
    }

    private static void validateService(String service) {
        if (!Arrays.asList("FM", "DM", "OM", "TM").contains(service)) {
            throw new IllegalArgumentException("invalid service: " + service);
        }
    }
}
