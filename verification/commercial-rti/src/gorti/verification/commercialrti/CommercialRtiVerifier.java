package gorti.verification.commercialrti;

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
import hla.rti1516e.exceptions.RTIinternalError;
import hla.rti1516e.time.HLAfloat64Time;
import hla.rti1516e.time.HLAfloat64TimeFactory;

import java.io.BufferedWriter;
import java.io.Closeable;
import java.io.IOException;
import java.math.BigInteger;
import java.nio.ByteBuffer;
import java.nio.ByteOrder;
import java.nio.charset.StandardCharsets;
import java.nio.file.AtomicMoveNotSupportedException;
import java.nio.file.DirectoryStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.nio.file.StandardCopyOption;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.TreeMap;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;
import java.util.function.BooleanSupplier;

/** Noninteractive federate verifier for the IEEE 1516-2010 Java API. */
@SuppressWarnings("rawtypes")
public final class CommercialRtiVerifier extends NullFederateAmbassador {
    private static final String DEFAULT_OBJECT_CLASS = "VerifierEntity";
    private static final String DEFAULT_INTERACTION_CLASS = "VerifierMessage";
    private static final String SUBSCRIBER_READY_INTERACTION_CLASS =
            "VerifierSubscriberReady";
    private static final String PUBLISHER_ACK_INTERACTION_CLASS =
            "VerifierPublisherAck";
    private static final String DEFAULT_OBJECT_INSTANCE_NAME = "CommercialRtiVerifierEntity";
    private static final String DECLARATIONS_SYNC = "VERIFY_DECLARATIONS";
    private static final String CONTROL_SYNC = "VERIFY_CONTROL";
    private static final String READY_SYNC = "VERIFY_READY";
    private static final String MEASURE_SYNC = "VERIFY_MEASURE";
    private static final String START_SYNC = "VERIFY_START";
    private static final String DONE_SYNC = "VERIFY_DONE";
    private static final String PUBLISHER_NAME = "CommercialRtiVerifierPublisher";
    private static final String SUBSCRIBER_NAME = "CommercialRtiVerifierSubscriber";
    private static final String PUBLISHER_RESIGNED_MARKER =
            ".portico-publisher-resigned.ready";
    private static final String SUBSCRIBER_DISCONNECTED_MARKER =
            ".portico-subscriber-disconnected.ready";
    private static final long CONTROL_RETRY_MILLIS = 100L;

    private final Config config;
    private final SemanticLogger semantic;
    private final MetricLogger metrics;
    private final SampleLogger samples;
    private final Map<Integer, Observation> reflected = new ConcurrentHashMap<Integer, Observation>();
    private final Map<Integer, Observation> received = new ConcurrentHashMap<Integer, Observation>();
    private final Map<Integer, Observation> warmupReflected =
            new ConcurrentHashMap<Integer, Observation>();
    private final Map<Integer, Observation> warmupReceived =
            new ConcurrentHashMap<Integer, Observation>();
    private final Map<Integer, GrantEvidence> grantEvidence =
            new ConcurrentHashMap<Integer, GrantEvidence>();
    private final List<EncodedIteration> workload = new ArrayList<EncodedIteration>();
    private final List<EncodedIteration> warmupWorkload = new ArrayList<EncodedIteration>();
    private final List<Long> updateAttributeDurations;
    private final List<Long> sendInteractionDurations;
    private final AtomicInteger duplicateCallbacks = new AtomicInteger();
    private final AtomicInteger invalidCallbacks = new AtomicInteger();
    private final AtomicInteger nextExpectedAttributeIndex = new AtomicInteger();
    private final AtomicInteger nextExpectedInteractionIndex = new AtomicInteger();
    private final AtomicInteger nextExpectedCallbackOrdinal = new AtomicInteger();
    private final AtomicReference<Throwable> callbackFailure = new AtomicReference<Throwable>();
    private final Object callbackSignal = new Object();
    private final Object attributeDigestLock = new Object();
    private final Object interactionDigestLock = new Object();
    private final Object callbackTraceLock = new Object();
    private final MessageDigest attributeCallbackDigest;
    private final MessageDigest interactionCallbackDigest;
    private final MessageDigest callbackTraceDigest;

    private RTIambassador rti;
    private EncoderFactory encoders;
    private HLAfloat64TimeFactory timeFactory;
    private ObjectClassHandle objectClass;
    private AttributeHandle objectSequence;
    private AttributeHandle objectPayload;
    private InteractionClassHandle interactionClass;
    private ParameterHandle interactionSequence;
    private ParameterHandle interactionPayload;
    private InteractionClassHandle subscriberReadyInteractionClass;
    private ParameterHandle subscriberReadyParticipantIndex;
    private InteractionClassHandle publisherAckInteractionClass;
    private ParameterHandle publisherAckParticipantIndex;
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
    private volatile ObjectClassHandle discoveredObjectClass;
    private volatile String discoveredObjectName;
    private volatile boolean objectRemoved;
    private volatile long removedTime;
    private volatile boolean nameReservationComplete;
    private volatile boolean nameReservationSucceeded;
    private volatile boolean declarationsAnnounced;
    private volatile boolean declarationsSynchronized;
    private volatile boolean controlAnnounced;
    private volatile boolean controlSynchronized;
    private final Set<Integer> controlReadyParticipants =
            Collections.newSetFromMap(new ConcurrentHashMap<Integer, Boolean>());
    private volatile boolean controlAcknowledged;
    private volatile boolean readyAnnounced;
    private volatile boolean readySynchronized;
    private volatile boolean measureAnnounced;
    private volatile boolean measureSynchronized;
    private volatile boolean startAnnounced;
    private volatile boolean startSynchronized;
    private volatile boolean doneAnnounced;
    private volatile boolean doneSynchronized;
    private volatile long receiveOrderBatchStartedNanos = -1L;
    private volatile long receiveOrderBatchDurationNanos = -1L;
    private volatile String synchronizationRegistrationSucceeded;
    private volatile String synchronizationFailure;

    public static void main(String[] args) {
        Config config = null;
        boolean compactOutputPrepared = false;
        try {
            config = Config.parse(args);
            prepareOutputDirectory(config);
            compactOutputPrepared = config.compactSummary;
            try (SemanticLogger semantic = new SemanticLogger(
                    config.compactSummary ? null
                            : config.outputDirectory.resolve(config.role + "-semantic.ndjson"),
                    config.role);
                 MetricLogger metrics = new MetricLogger(
                    config.compactSummary ? null
                            : config.outputDirectory.resolve(config.role + "-metrics.ndjson"));
                 SampleLogger samples = new SampleLogger(
                    config.compactSummary ? null
                            : config.outputDirectory.resolve(config.role + "-samples.ndjson"))) {
                new CommercialRtiVerifier(config, semantic, metrics, samples).execute();
            }
            if (config.compactSummary) {
                verifyCompactOutput(config);
            }
        } catch (Throwable failure) {
            if (config != null && compactOutputPrepared) {
                deleteSummaryBestEffort(config);
            }
            System.err.println("reference RTI verifier failed: " + failure.getClass().getSimpleName()
                    + ": " + String.valueOf(failure.getMessage()));
            System.exit(1);
        }
        // Some RTIs retain non-daemon transport threads briefly after disconnect.
        System.exit(0);
    }

    private static void prepareOutputDirectory(Config config) throws IOException {
        Files.createDirectories(config.outputDirectory);
        if (!config.compactSummary) {
            return;
        }
        try (DirectoryStream<Path> entries = Files.newDirectoryStream(config.outputDirectory)) {
            for (Path entry : entries) {
                throw new IllegalArgumentException(
                        "compact output directory must be empty: " + entry.getFileName());
            }
        }
    }

    private static void verifyCompactOutput(Config config) throws IOException {
        int entryCount = 0;
        try (DirectoryStream<Path> entries = Files.newDirectoryStream(config.outputDirectory)) {
            for (Path entry : entries) {
                entryCount++;
                if (!entry.getFileName().equals(config.summaryPath().getFileName())
                        || !Files.isRegularFile(entry)) {
                    throw new IllegalStateException(
                            "compact output contains an unexpected entry: " + entry.getFileName());
                }
            }
        }
        if (entryCount != 1) {
            throw new IllegalStateException(
                    "compact output must contain exactly " + config.summaryPath().getFileName());
        }
    }

    private static void deleteSummaryBestEffort(Config config) {
        try {
            Files.deleteIfExists(config.summaryPath());
            Files.deleteIfExists(config.summaryTemporaryPath());
        } catch (IOException ignored) {
            // Preserve the original failure while avoiding a partially accepted result.
        }
    }

    private CommercialRtiVerifier(Config config, SemanticLogger semantic, MetricLogger metrics,
            SampleLogger samples) {
        this.config = config;
        this.semantic = semantic;
        this.metrics = metrics;
        this.samples = samples;
        this.updateAttributeDurations = config.compactSummary
                ? new ArrayList<Long>() : Collections.<Long>emptyList();
        this.sendInteractionDurations = config.compactSummary
                ? new ArrayList<Long>() : Collections.<Long>emptyList();
        this.attributeCallbackDigest = config.compactSummary ? newSha256() : null;
        this.interactionCallbackDigest = config.compactSummary ? newSha256() : null;
        this.callbackTraceDigest = config.compactSummary ? newSha256() : null;
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
            createJoinReadyMarker();
            if (config.isPublisher()) {
                prepareWorkload();
            }
            if (!config.receiveOrder) {
                enableTimeManagement();
            }
            if (config.startupReleaseFile != null) {
                createStartupReadyMarker();
                awaitStartupReleaseMarker();
            }
            if (isCompactReceiveOrderWorkload() && config.participantCount > 2) {
                synchronize(DECLARATIONS_SYNC);
                reassertDeclarationInterests();
                completeControlHandshake();
                synchronize(CONTROL_SYNC);
            }
            prepareCompactReceiveOrderReady();
            if (config.startupReleaseFile == null) {
                createStartupReadyMarker();
            }
            synchronize(READY_SYNC);

            if (config.operationWarmup > 0) {
                if (config.isPublisher()) {
                    if (!isCompactReceiveOrderWorkload()) {
                        registerPublisherObject();
                    }
                    publishWarmup();
                } else {
                    awaitWarmupCallbacks();
                }
                synchronize(MEASURE_SYNC);
            } else if (config.receiveOrder && config.workloadPlan != null) {
                synchronize(MEASURE_SYNC);
            }

            if (config.receiveOrder && config.workloadPlan != null) {
                if (config.isPublisher() && !isCompactReceiveOrderWorkload()) {
                    registerPublisherObject();
                }
                synchronize(START_SYNC);
            }

            if (!config.receiveOrder) {
                if (config.isPublisher() && !config.tmAdvanceOnly) {
                    publishTraffic();
                }
                synchronize(MEASURE_SYNC);
                if (config.tmAdvanceOnly) {
                    advanceOnly();
                } else if (config.isPublisher()) {
                    advancePublishedTimestampOrderTraffic();
                } else {
                    consumeTraffic();
                }
            } else if (config.isPublisher()) {
                publishTraffic();
            } else {
                consumeTraffic();
            }

            synchronize(DONE_SYNC);
            if (config.receiveOrder) {
                completeReceiveOrderRemoval();
            }
            semantic.event("FM", "phase", data("phase", "do", "status", "complete"));
            semantic.event("FM", "phase", data("phase", "review", "status", "start"));
            review();
            semantic.event("FM", "phase", data(
                    "count", config.count,
                    "phase", "review",
                    "status", "complete"));
            orderlyShutdown();
            semantic.event("FM", "phase", data(
                    "phase", "reflect",
                    "result", "pass",
                    "status", "complete"));
            if (config.compactSummary) {
                writeCompactSummary();
            }
            passed = true;
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
                rti.connect(CommercialRtiVerifier.this, CallbackModel.HLA_IMMEDIATE,
                        config.localSettingsDesignator);
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
        semantic.event("FM", "federation_ready", data(
                "fom", config.fom.getFileName().toString()));

        final String federateName = config.federateName();
        timed("FM", "join_federation_execution", new CheckedRunnable() {
            public void run() throws Exception {
                selfHandle = rti.joinFederationExecution(federateName, "CommercialRtiVerifier-" + config.role,
                        config.federationName);
            }
        });
        joined = true;
        semantic.event("FM", "joined", data("federate_type", "CommercialRtiVerifier-" + config.role));
        timeFactory = (HLAfloat64TimeFactory) rti.getTimeFactory();
    }

    private void resolveHandlesAndDeclareInterests() throws Exception {
        objectClass = rti.getObjectClassHandle(config.objectClass);
        objectSequence = rti.getAttributeHandle(objectClass, "Sequence");
        objectPayload = rti.getAttributeHandle(objectClass, "Payload");
        interactionClass = rti.getInteractionClassHandle(config.interactionClass);
        interactionSequence = rti.getParameterHandle(interactionClass, "Sequence");
        interactionPayload = rti.getParameterHandle(interactionClass, "Payload");
        if (usesControlHandshake()) {
            subscriberReadyInteractionClass = rti.getInteractionClassHandle(
                    SUBSCRIBER_READY_INTERACTION_CLASS);
            subscriberReadyParticipantIndex = rti.getParameterHandle(
                    subscriberReadyInteractionClass, "ParticipantIndex");
            publisherAckInteractionClass = rti.getInteractionClassHandle(
                    PUBLISHER_ACK_INTERACTION_CLASS);
            publisherAckParticipantIndex = rti.getParameterHandle(
                    publisherAckInteractionClass, "ParticipantIndex");
        }

        final AttributeHandleSet attributes = rti.getAttributeHandleSetFactory().create();
        attributes.add(objectSequence);
        attributes.add(objectPayload);
        if (config.isPublisher()) {
            timed("DM", "publish_object_class_attributes", new CheckedRunnable() {
                public void run() throws Exception {
                    rti.publishObjectClassAttributes(objectClass, attributes);
                }
            });
            semantic.event("DM", "object_published", data("class", config.objectClass));
            timed("DM", "publish_interaction_class", new CheckedRunnable() {
                public void run() throws Exception {
                    rti.publishInteractionClass(interactionClass);
                }
            });
            semantic.event("DM", "interaction_published", data("class", config.interactionClass));
        } else {
            timed("DM", "subscribe_object_class_attributes", new CheckedRunnable() {
                public void run() throws Exception {
                    rti.subscribeObjectClassAttributes(objectClass, attributes);
                }
            });
            semantic.event("DM", "object_subscribed", data("class", config.objectClass));
            timed("DM", "subscribe_interaction_class", new CheckedRunnable() {
                public void run() throws Exception {
                    rti.subscribeInteractionClass(interactionClass);
                }
            });
            semantic.event("DM", "interaction_subscribed", data("class", config.interactionClass));
        }
        if (usesControlHandshake()) {
            if (config.isPublisher()) {
                rti.subscribeInteractionClass(subscriberReadyInteractionClass);
                rti.publishInteractionClass(publisherAckInteractionClass);
            } else {
                rti.publishInteractionClass(subscriberReadyInteractionClass);
                rti.subscribeInteractionClass(publisherAckInteractionClass);
            }
        }
    }

    private void prepareWorkload() throws Exception {
        workload.clear();
        warmupWorkload.clear();
        if (config.workloadPlan == null) {
            for (int index = 0; index < config.count; index++) {
                workload.add(encodeIteration(index));
            }
        } else {
            for (PlanRecord record : config.workloadPlan.records) {
                workload.add(encodeIteration(
                        record.index, record.attributePayload, record.interactionPayload));
            }
        }
        for (int offset = 0; offset < config.operationWarmup; offset++) {
            warmupWorkload.add(encodeIteration(config.count + offset));
        }
    }

    private EncodedIteration encodeIteration(int index) throws Exception {
        String objectValue = deterministicPayload(config.seed, "attribute", index);
        String interactionValue = deterministicPayload(config.seed, "interaction", index);
        return encodeIteration(index, objectValue, interactionValue);
    }

    private EncodedIteration encodeIteration(int index, String objectValue,
            String interactionValue) throws Exception {
        long logicalTime = index + 1L;
        byte[] sequence = encodeInteger(index);

        AttributeHandleValueMap attributes = rti.getAttributeHandleValueMapFactory().create(2);
        attributes.put(objectSequence, sequence);
        attributes.put(objectPayload, encodeString(objectValue));
        ParameterHandleValueMap parameters = rti.getParameterHandleValueMapFactory().create(2);
        parameters.put(interactionSequence, sequence);
        parameters.put(interactionPayload, encodeString(interactionValue));

        return new EncodedIteration(
                index,
                logicalTime,
                config.receiveOrder ? null : timeFactory.makeTime((double) logicalTime),
                objectValue,
                interactionValue,
                attributes,
                parameters);
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
        final boolean registrar = config.registersSynchronization(label);
        if (registrar) {
            final String peerName = config.isPublisher() ? SUBSCRIBER_NAME : PUBLISHER_NAME;
            final String peerActor = config.isPublisher() ? "subscriber" : "publisher";
            synchronizationRegistrationSucceeded = null;
            synchronizationFailure = null;
            if (config.allFederatesSynchronization) {
                if (config.participantCount > 2) {
                    awaitAllFederatesPresent();
                }
                timed("FM", "register_synchronization_point", new CheckedRunnable() {
                    public void run() throws Exception {
                        rti.registerFederationSynchronizationPoint(label, null);
                    }
                });
            } else {
                final FederateHandle peerHandle = awaitFederatePresent(peerName);
                final FederateHandleSet participants = rti.getFederateHandleSetFactory().create();
                participants.add(selfHandle);
                participants.add(peerHandle);
                timed("FM", "register_synchronization_point", new CheckedRunnable() {
                    public void run() throws Exception {
                        rti.registerFederationSynchronizationPoint(label, null, participants);
                    }
                });
            }
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
                    "participants", config.participantCount));
        }

        await("synchronization point announced: " + label, new BooleanSupplier() {
            public boolean getAsBoolean() {
                return synchronizationAnnounced(label) || synchronizationFailure != null;
            }
        });
        if (synchronizationFailure != null) {
            throw new IllegalStateException(synchronizationFailure);
        }
        semantic.event("FM", "synchronization_announced", data("label", label));

        if (READY_SYNC.equals(label)) {
            validateCompactReceiveOrderReady();
        }
        if (START_SYNC.equals(label) && !config.isPublisher()) {
            if (receiveOrderBatchStartedNanos >= 0L) {
                throw new IllegalStateException("receive-order batch timer was already armed");
            }
            receiveOrderBatchStartedNanos = System.nanoTime();
        }
        timed("FM", "synchronization_point_achieved", new CheckedRunnable() {
            public void run() throws Exception {
                rti.synchronizationPointAchieved(label);
            }
        });
        semantic.event("FM", "synchronization_achieved", data("label", label));
        await("federation synchronized: " + label, new BooleanSupplier() {
            public boolean getAsBoolean() {
                return synchronizationComplete(label);
            }
        });
        semantic.event("FM", "federation_synchronized", data("label", label));
    }

    private boolean isCompactReceiveOrderWorkload() {
        return config.compactSummary && config.receiveOrder && config.workloadPlan != null;
    }

    private boolean usesControlHandshake() {
        return isCompactReceiveOrderWorkload() && config.participantCount > 2;
    }

    private void reassertDeclarationInterests() throws Exception {
        AttributeHandleSet attributes = rti.getAttributeHandleSetFactory().create();
        attributes.add(objectSequence);
        attributes.add(objectPayload);
        if (config.isPublisher()) {
            rti.publishObjectClassAttributes(objectClass, attributes);
            rti.publishInteractionClass(interactionClass);
            rti.subscribeInteractionClass(subscriberReadyInteractionClass);
            rti.publishInteractionClass(publisherAckInteractionClass);
        } else {
            rti.subscribeObjectClassAttributes(objectClass, attributes);
            rti.subscribeInteractionClass(interactionClass);
            rti.publishInteractionClass(subscriberReadyInteractionClass);
            rti.subscribeInteractionClass(publisherAckInteractionClass);
        }
    }

    private void completeControlHandshake() throws Exception {
        if (!usesControlHandshake()) {
            return;
        }
        if (config.isPublisher()) {
            awaitControlReadyParticipants();
            for (int repeat = 0; repeat < 3; repeat++) {
                for (int index = 1; index < config.participantCount; index++) {
                    sendPublisherAcknowledgement(index);
                }
                if (repeat < 2) {
                    Thread.sleep(CONTROL_RETRY_MILLIS);
                }
            }
            return;
        }

        long deadline = System.nanoTime() + config.timeoutMillis * 1_000_000L;
        while (!controlAcknowledged) {
            sendSubscriberReady();
            checkCallbackFailure();
            if (System.nanoTime() >= deadline) {
                throw new IllegalStateException(
                        "timed out waiting for control acknowledgement for subscriber-"
                                + config.participantIndex);
            }
            synchronized (callbackSignal) {
                if (!controlAcknowledged) {
                    callbackSignal.wait(CONTROL_RETRY_MILLIS);
                }
            }
        }
        checkCallbackFailure();
    }

    private void awaitControlReadyParticipants() throws Exception {
        long deadline = System.nanoTime() + config.timeoutMillis * 1_000_000L;
        while (controlReadyParticipants.size() < config.participantCount - 1) {
            checkCallbackFailure();
            if (System.nanoTime() >= deadline) {
                throw new IllegalStateException(
                        "timed out waiting for subscriber control readiness: received "
                                + controlReadyParticipants.size() + " of "
                                + (config.participantCount - 1));
            }
            synchronized (callbackSignal) {
                if (controlReadyParticipants.size() < config.participantCount - 1) {
                    callbackSignal.wait(CONTROL_RETRY_MILLIS);
                }
            }
        }
        checkCallbackFailure();
    }

    private void sendSubscriberReady() throws Exception {
        ParameterHandleValueMap parameters =
                rti.getParameterHandleValueMapFactory().create(1);
        parameters.put(subscriberReadyParticipantIndex,
                encodeInteger(config.participantIndex));
        rti.sendInteraction(subscriberReadyInteractionClass, parameters, null);
    }

    private void sendPublisherAcknowledgement(int participantIndex) throws Exception {
        ParameterHandleValueMap parameters =
                rti.getParameterHandleValueMapFactory().create(1);
        parameters.put(publisherAckParticipantIndex, encodeInteger(participantIndex));
        rti.sendInteraction(publisherAckInteractionClass, parameters, null);
    }

    private void prepareCompactReceiveOrderReady() throws Exception {
        if (!isCompactReceiveOrderWorkload()) {
            return;
        }
        if (config.isPublisher()) {
            registerPublisherObject();
        } else {
            await("publisher object discovery before " + READY_SYNC, new BooleanSupplier() {
                public boolean getAsBoolean() {
                    return objectDiscovered;
                }
            });
        }
        validateCompactReceiveOrderReady();
    }

    private void createStartupReadyMarker() throws IOException {
        if (config.startupReadyFile == null) {
            return;
        }
        Path parent = config.startupReadyFile.getParent();
        Files.createDirectories(parent);
        Path resolvedMarker = parent.toRealPath().resolve(config.startupReadyFile.getFileName());
        Path resolvedOutputDirectory = config.outputDirectory.toRealPath();
        if (resolvedMarker.startsWith(resolvedOutputDirectory)) {
            throw new IllegalArgumentException(
                    "--startup-ready-file must be outside the compact output directory");
        }
        Files.createFile(config.startupReadyFile);
    }

    private void createJoinReadyMarker() throws IOException {
        if (config.joinReadyFile == null) {
            return;
        }
        Path parent = config.joinReadyFile.getParent();
        Files.createDirectories(parent);
        Path resolvedMarker = parent.toRealPath().resolve(config.joinReadyFile.getFileName());
        Path resolvedOutputDirectory = config.outputDirectory.toRealPath();
        if (resolvedMarker.startsWith(resolvedOutputDirectory)) {
            throw new IllegalArgumentException(
                    "--join-ready-file must be outside the compact output directory");
        }
        Files.createFile(config.joinReadyFile);
    }

    private void awaitStartupReleaseMarker() throws Exception {
        if (config.startupReleaseFile == null) {
            return;
        }
        long deadline = System.nanoTime() + config.timeoutMillis * 1_000_000L;
        while (!Files.exists(config.startupReleaseFile)) {
            checkCallbackFailure();
            if (System.nanoTime() >= deadline) {
                throw new IllegalStateException(
                        "timed out waiting for startup release marker");
            }
            Thread.sleep(10L);
        }
        if (!Files.isRegularFile(config.startupReleaseFile)) {
            throw new IllegalStateException(
                    "startup release marker is not a regular file");
        }
        Files.delete(config.startupReleaseFile);
        if (Files.exists(config.startupReleaseFile)) {
            throw new IllegalStateException(
                    "startup release marker was not consumed");
        }
    }

    private void validateCompactReceiveOrderReady() {
        if (!isCompactReceiveOrderWorkload()) {
            return;
        }
        if (config.isPublisher()) {
            if (objectInstance == null) {
                throw new IllegalStateException(
                        "publisher object must be registered before " + READY_SYNC);
            }
            return;
        }
        if (!objectDiscovered || discoveredObject == null) {
            throw new IllegalStateException(
                    "subscriber must discover the publisher object before " + READY_SYNC);
        }
        if (objectClass == null || !objectClass.equals(discoveredObjectClass)) {
            throw new IllegalStateException(
                    "subscriber discovered an unexpected object class before " + READY_SYNC);
        }
        if (!config.objectInstanceName.equals(discoveredObjectName)) {
            throw new IllegalStateException(
                    "subscriber discovered an unexpected object name before " + READY_SYNC);
        }
    }

    private boolean synchronizationAnnounced(String label) {
        if (DECLARATIONS_SYNC.equals(label)) {
            return declarationsAnnounced;
        }
        if (CONTROL_SYNC.equals(label)) {
            return controlAnnounced;
        }
        if (READY_SYNC.equals(label)) {
            return readyAnnounced;
        }
        if (MEASURE_SYNC.equals(label)) {
            return measureAnnounced;
        }
        if (START_SYNC.equals(label)) {
            return startAnnounced;
        }
        return DONE_SYNC.equals(label) && doneAnnounced;
    }

    private boolean synchronizationComplete(String label) {
        if (DECLARATIONS_SYNC.equals(label)) {
            return declarationsSynchronized;
        }
        if (CONTROL_SYNC.equals(label)) {
            return controlSynchronized;
        }
        if (READY_SYNC.equals(label)) {
            return readySynchronized;
        }
        if (MEASURE_SYNC.equals(label)) {
            return measureSynchronized;
        }
        if (START_SYNC.equals(label)) {
            return startSynchronized;
        }
        return DONE_SYNC.equals(label) && doneSynchronized;
    }

    private void registerPublisherObject() throws Exception {
        if (objectInstance != null) {
            return;
        }
        nameReservationComplete = false;
        nameReservationSucceeded = false;
        timed("OM", "reserve_object_instance_name", new CheckedRunnable() {
            public void run() throws Exception {
                rti.reserveObjectInstanceName(config.objectInstanceName);
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
        semantic.event("OM", "object_name_reserved", data("name", config.objectInstanceName));

        timed("OM", "register_object_instance", new CheckedRunnable() {
            public void run() throws Exception {
                objectInstance = rti.registerObjectInstance(
                        objectClass, config.objectInstanceName);
            }
        });
        semantic.event("OM", "object_registered", data(
                "class", config.objectClass,
                "name", config.objectInstanceName));
    }

    private void publishWarmup() throws Exception {
        for (final EncodedIteration iteration : warmupWorkload) {
            rti.updateAttributeValues(objectInstance, iteration.attributes, null);
            rti.sendInteraction(interactionClass, iteration.parameters, null);
        }
    }

    private void awaitWarmupCallbacks() throws Exception {
        await("operation warmup callbacks", new BooleanSupplier() {
            public boolean getAsBoolean() {
                return warmupReflected.size() == config.operationWarmup
                        && warmupReceived.size() == config.operationWarmup;
            }
        });
        checkCallbackFailure();
    }

    private void publishTraffic() throws Exception {
        if (!isCompactReceiveOrderWorkload()) {
            registerPublisherObject();
        }

        for (final EncodedIteration iteration : workload) {
            benchmarkTimed("OM", "updateAttributeValues", "update_attribute_values",
                    new CheckedRunnable() {
                public void run() throws Exception {
                    if (config.receiveOrder) {
                        rti.updateAttributeValues(objectInstance, iteration.attributes, null);
                    } else {
                        rti.updateAttributeValues(
                                objectInstance, iteration.attributes, null, iteration.timestamp);
                    }
                }
            });
            if (!config.compactSummary) {
                semantic.event("OM", "attributes_updated", config.receiveOrder
                        ? data("index", iteration.index, "order", "receive",
                                "payload", iteration.objectValue)
                        : data("index", iteration.index, "logical_time", iteration.logicalTime,
                                "payload", iteration.objectValue));
            }

            benchmarkTimed("OM", "sendInteraction", "send_interaction",
                    new CheckedRunnable() {
                public void run() throws Exception {
                    if (config.receiveOrder) {
                        rti.sendInteraction(interactionClass, iteration.parameters, null);
                    } else {
                        rti.sendInteraction(
                                interactionClass, iteration.parameters, null, iteration.timestamp);
                    }
                }
            });
            if (!config.compactSummary) {
                semantic.event("OM", "interaction_sent", config.receiveOrder
                        ? data("index", iteration.index, "order", "receive",
                                "payload", iteration.interactionValue)
                        : data("index", iteration.index, "logical_time", iteration.logicalTime,
                                "payload", iteration.interactionValue));
            }
        }
    }

    private void advancePublishedTimestampOrderTraffic() throws Exception {
        for (final EncodedIteration iteration : workload) {
            advanceTo(iteration.logicalTime, true);
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
                "name", config.objectInstanceName));
        advanceTo(removalTime, false);
    }

    private void advanceOnly() throws Exception {
        for (long logicalTime = 1L; logicalTime <= config.count; logicalTime++) {
            advanceTo(logicalTime, true);
        }
    }

    private void consumeTraffic() throws Exception {
        if (config.receiveOrder) {
            consumeReceiveOrderTraffic();
            return;
        }
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
                if (!config.objectInstanceName.equals(discoveredObjectName)) {
                    throw new IllegalStateException("unexpected object instance name");
                }
                semantic.event("OM", "object_discovered", data(
                        "class", config.objectClass,
                        "name", config.objectInstanceName));
            }

            Observation objectObservation = reflected.get(item);
            Observation interactionObservation = received.get(item);
            long batchCompleted = Math.max(objectObservation.completedAtNanos,
                    interactionObservation.completedAtNanos);
            lastDeliveryCompleted = batchCompleted;
            assertObservation("attribute", item, logicalTime, objectObservation);
            assertObservation("interaction", item, logicalTime, interactionObservation);
            if (!config.compactSummary) {
                semantic.event("OM", "attributes_reflected", data(
                        "index", item,
                        "logical_time", logicalTime,
                        "payload", objectObservation.payload));
                semantic.event("OM", "interaction_received", data(
                        "index", item,
                        "logical_time", logicalTime,
                        "payload", interactionObservation.payload));
            }
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
                "name", config.objectInstanceName));
    }

    private void consumeReceiveOrderTraffic() throws Exception {
        long batchStarted = config.workloadPlan == null
                ? System.nanoTime() : receiveOrderBatchStartedNanos;
        if (batchStarted < 0L) {
            throw new IllegalStateException("receive-order batch timer was not armed");
        }
        long lastDeliveryCompleted = batchStarted;
        for (int index = 0; index < config.count; index++) {
            final int item = index;
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
                if (!config.objectInstanceName.equals(discoveredObjectName)) {
                    throw new IllegalStateException("unexpected object instance name");
                }
                semantic.event("OM", "object_discovered", data(
                        "class", config.objectClass,
                        "name", config.objectInstanceName));
            }
            Observation objectObservation = reflected.get(item);
            Observation interactionObservation = received.get(item);
            lastDeliveryCompleted = Math.max(lastDeliveryCompleted,
                    Math.max(objectObservation.completedAtNanos,
                            interactionObservation.completedAtNanos));
            assertObservation("attribute", item, -1L, objectObservation);
            assertObservation("interaction", item, -1L, interactionObservation);
            if (!config.compactSummary) {
                semantic.event("OM", "attributes_reflected", data(
                        "index", item,
                        "order", "receive",
                        "payload", objectObservation.payload));
                semantic.event("OM", "interaction_received", data(
                        "index", item,
                        "order", "receive",
                        "payload", interactionObservation.payload));
            }
        }
        long batchDuration = Math.max(1L, lastDeliveryCompleted - batchStarted);
        receiveOrderBatchDurationNanos = batchDuration;
        metrics.metric("OM", "completed_receive_order_batch", "nanoseconds", batchDuration);
        samples.sample("completedReceiveOrderBatch", batchDuration, "OM", "delivery");
        metrics.metric("OM", "sustained_throughput", "deliveries_per_second",
                (config.count * 2.0d * 1_000_000_000.0d) / batchDuration);
    }

    private void completeReceiveOrderRemoval() throws Exception {
        if (config.isPublisher()) {
            timed("OM", "delete_object_instance", new CheckedRunnable() {
                public void run() throws Exception {
                    rti.deleteObjectInstance(objectInstance, null);
                }
            });
            semantic.event("OM", "object_deleted", data(
                    "name", config.objectInstanceName,
                    "order", "receive"));
            return;
        }
        await("object removal", new BooleanSupplier() {
            public boolean getAsBoolean() {
                return objectRemoved;
            }
        });
        semantic.event("OM", "object_removed", data(
                "name", config.objectInstanceName,
                "order", "receive"));
    }

    private long advanceTo(final long logicalTime, boolean benchmarkSample) throws Exception {
        HLAfloat64Time requestedTime = timeFactory.makeTime((double) logicalTime);
        final int deliveryBatch = !config.isPublisher() && benchmarkSample
                && !config.tmAdvanceOnly
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
            if (benchmarkSample) {
                samples.grantBoundarySample(System.nanoTime() - started);
            }
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
        if (!readySynchronized || !doneSynchronized
                || ((config.operationWarmup > 0 || config.workloadPlan != null)
                        && !measureSynchronized)
                || (config.receiveOrder && config.workloadPlan != null && !startSynchronized)) {
            throw new IllegalStateException("required synchronization did not complete");
        }
        if (config.isPublisher()) {
            if (!config.tmAdvanceOnly && objectInstance == null) {
                throw new IllegalStateException("publisher did not register its object");
            }
            if (config.compactSummary
                    && (updateAttributeDurations.size() != config.count
                            || sendInteractionDurations.size() != config.count
                            || !reflected.isEmpty() || !received.isEmpty()
                            || !warmupReflected.isEmpty() || !warmupReceived.isEmpty()
                            || duplicateCallbacks.get() != 0 || invalidCallbacks.get() != 0)) {
                throw new IllegalStateException("publisher compact evidence was incomplete");
            }
        } else {
            if ((!config.tmAdvanceOnly
                    && (!objectDiscovered || reflected.size() != config.count
                            || received.size() != config.count))
                    || warmupReflected.size() != config.operationWarmup
                    || warmupReceived.size() != config.operationWarmup
                    || (!config.receiveOrder && !config.tmAdvanceOnly
                            && grantEvidence.size() != config.count)
                    || duplicateCallbacks.get() != 0
                    || invalidCallbacks.get() != 0) {
                throw new IllegalStateException("subscriber callback totals did not match count");
            }
            if (config.compactSummary && receiveOrderBatchDurationNanos < 1L) {
                throw new IllegalStateException("subscriber receive-order batch was not measured");
            }
            if (config.compactSummary
                    && (nextExpectedAttributeIndex.get() != config.count
                            || nextExpectedInteractionIndex.get() != config.count)) {
                throw new IllegalStateException("subscriber callback order evidence was incomplete");
            }
            metrics.metric("OM", "attributes_reflected", "count", reflected.size());
            metrics.metric("OM", "interactions_received", "count", received.size());
            metrics.metric("OM", "delivery_accounting.duplicates", "count",
                    duplicateCallbacks.get());
            metrics.metric("OM", "delivery_accounting.invalid", "count",
                    invalidCallbacks.get());
            if (!config.receiveOrder && !config.tmAdvanceOnly) {
                metrics.metric("TM", "callback_before_grant_guard.passed", "count",
                        grantEvidence.size());
            }
        }
        if (!config.receiveOrder) {
            long expectedFinalTime = config.tmAdvanceOnly ? config.count : config.count + 1L;
            if (grantedTime != expectedFinalTime) {
                throw new IllegalStateException("final logical time did not match count");
            }
            metrics.metric("TM", "final_logical_time", "ticks", grantedTime);
        }
    }

    private void writeCompactSummary() throws IOException {
        if (config.workloadPlan == null || config.numericSeed == null) {
            throw new IllegalStateException("compact summary requires a validated workload plan");
        }

        long expectedAttributes = config.isPublisher() ? 0L : config.count;
        long expectedInteractions = config.isPublisher() ? 0L : config.count;
        long acceptedAttributes = reflected.size();
        long acceptedInteractions = received.size();
        long expected = expectedAttributes + expectedInteractions;
        long delivered = acceptedAttributes + acceptedInteractions;
        long duplicates = duplicateCallbacks.get();
        long invalid = invalidCallbacks.get();
        long rejected = duplicates + invalid;
        long dropped = Math.max(0L, expected - delivered - rejected);

        Map<String, Object> callbackAccounting = data(
                "attribute_delivered", acceptedAttributes,
                "delivered", delivered,
                "dropped", dropped,
                "duplicates", duplicates,
                "expected", expected,
                "interaction_delivered", acceptedInteractions,
                "invalid", invalid,
                "rejected", rejected,
                "unexpected", invalid);

        if (!readySynchronized || !measureSynchronized || !startSynchronized
                || !doneSynchronized || rejected != 0L || dropped != 0L) {
            throw new IllegalStateException("compact summary evidence was not accepted");
        }

        Map<String, Object> summary = data(
                "attribute_arrival_order_sha256", callbackDigestHex(
                        attributeCallbackDigest, attributeDigestLock),
                "callback_accounting", callbackAccounting,
                "callback_trace_sha256", callbackDigestHex(
                        callbackTraceDigest, callbackTraceLock),
                "count", config.count,
                "done", doneSynchronized,
                "interaction_arrival_order_sha256", callbackDigestHex(
                        interactionCallbackDigest, interactionDigestLock),
                "measure", measureSynchronized,
                "plan_sha256", config.workloadPlan.planSha256,
                "ready", readySynchronized,
                "role", config.role,
                "schema", "gorti.devstone.participant-summary/v1",
                "seed", config.workloadPlan.seed,
                "start", startSynchronized,
                "status", "accepted",
                "topology_sha256", config.workloadPlan.topologySha256);
        if (config.isPublisher()) {
            summary.put("send_interaction_median_ns",
                    Long.valueOf(medianNanos(sendInteractionDurations)));
            summary.put("update_attribute_values_median_ns",
                    Long.valueOf(medianNanos(updateAttributeDurations)));
        } else {
            summary.put("completed_receive_order_batch_ns",
                    Long.valueOf(receiveOrderBatchDurationNanos));
        }

        Path temporary = config.summaryTemporaryPath();
        Path target = config.summaryPath();
        try {
            try (BufferedWriter writer = Files.newBufferedWriter(
                    temporary, StandardCharsets.UTF_8)) {
                writer.write(Json.object(summary));
                writer.newLine();
            }
            try {
                Files.move(temporary, target, StandardCopyOption.ATOMIC_MOVE,
                        StandardCopyOption.REPLACE_EXISTING);
            } catch (AtomicMoveNotSupportedException unsupported) {
                Files.move(temporary, target, StandardCopyOption.REPLACE_EXISTING);
            }
        } catch (IOException failure) {
            Files.deleteIfExists(temporary);
            throw failure;
        } catch (RuntimeException failure) {
            Files.deleteIfExists(temporary);
            throw failure;
        }
    }

    private static long medianNanos(List<Long> samples) {
        if (samples.isEmpty()) {
            throw new IllegalStateException("cannot calculate a median without samples");
        }
        List<Long> ordered = new ArrayList<Long>(samples);
        Collections.sort(ordered);
        int middle = ordered.size() / 2;
        if ((ordered.size() & 1) != 0) {
            return ordered.get(middle).longValue();
        }
        long lower = ordered.get(middle - 1).longValue();
        long upper = ordered.get(middle).longValue();
        return lower + (upper - lower) / 2L;
    }

    private static String callbackDigestHex(MessageDigest digest, Object lock) {
        synchronized (lock) {
            return lowercaseHex(digest.digest());
        }
    }

    private void orderlyShutdown() throws Exception {
        if (config.isPublisher() && config.participantCount > 2
                && config.teardownReadyFile != null) {
            for (int index = 1; index < config.participantCount; index++) {
                awaitAndConsumeTeardownMarker(
                        config.teardownReadyMarker(index),
                        "subscriber-" + index + " resigned");
            }
            semantic.event("FM", "peer_resigned", data(
                    "peer", "subscribers",
                    "count", config.participantCount - 1));
        } else if (config.isPublisher() && config.participantCount > 2) {
            for (int index = 1; index < config.participantCount; index++) {
                awaitFederateAbsent(config.subscriberFederateName(index));
            }
            semantic.event("FM", "peer_resigned", data(
                    "peer", "subscribers",
                    "count", config.participantCount - 1));
        } else if (config.isPublisher() && config.teardownReadyFile != null) {
            awaitAndConsumeTeardownMarker(
                    config.teardownReadyFile, "subscriber resigned");
            semantic.event("FM", "peer_resigned", data("peer", "subscriber"));
        } else if (config.isPublisher()) {
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

        if (config.teardownReadyFile != null) {
            if (config.isPublisher()) {
                for (int index = 1; index < config.participantCount; index++) {
                    createTeardownMarker(config.publisherResignedMarker(index));
                }
                for (int index = 1; index < config.participantCount; index++) {
                    awaitAndConsumeTeardownMarker(
                            config.subscriberDisconnectedMarker(index),
                            "subscriber-" + index + " disconnected");
                }
                semantic.event("FM", "peer_disconnected", data(
                        "peer", config.participantCount == 2 ? "subscriber" : "subscribers",
                        "count", config.participantCount - 1));
            } else {
                createTeardownMarker(
                        config.teardownReadyMarker(config.participantIndex));
                awaitAndConsumeTeardownMarker(
                        config.publisherResignedMarker(config.participantIndex),
                        "publisher resigned");
                semantic.event("FM", "peer_resigned", data("peer", "publisher"));
            }
        }

        if (config.isPublisher()) {
            destroyFederationWhenEmpty();
            semantic.event("FM", "federation_destroyed", Collections.<String, Object>emptyMap());
        }

        timed("FM", "disconnect", new CheckedRunnable() {
            public void run() throws Exception {
                rti.disconnect();
            }
        });
        connected = false;
        semantic.event("FM", "disconnected", Collections.<String, Object>emptyMap());

        if (!config.isPublisher() && config.teardownReadyFile != null) {
            createTeardownMarker(
                    config.subscriberDisconnectedMarker(config.participantIndex));
        }
    }

    private void awaitAndConsumeTeardownMarker(Path marker, String description)
            throws Exception {
        long deadline = System.nanoTime() + config.timeoutMillis * 1_000_000L;
        while (!Files.exists(marker)) {
            checkCallbackFailure();
            if (System.nanoTime() >= deadline) {
                throw new IllegalStateException(
                        "timed out waiting for teardown marker: " + description);
            }
            Thread.sleep(10L);
        }
        if (!Files.isRegularFile(marker)) {
            throw new IllegalStateException(
                    "teardown marker is not a regular file: " + description);
        }
        byte[] expected = teardownMarkerToken().getBytes(StandardCharsets.UTF_8);
        byte[] actual = Files.readAllBytes(marker);
        if (!Arrays.equals(expected, actual)) {
            throw new IllegalStateException(
                    "teardown marker token differs: " + description);
        }
        Files.delete(marker);
        if (Files.exists(marker)) {
            throw new IllegalStateException(
                    "teardown marker was not consumed: " + description);
        }
    }

    private void createTeardownMarker(Path marker) throws IOException {
        Path parent = marker.getParent();
        Files.createDirectories(parent);
        Path resolvedMarker = parent.toRealPath().resolve(marker.getFileName());
        Path resolvedOutputDirectory = config.outputDirectory.toRealPath();
        if (resolvedMarker.startsWith(resolvedOutputDirectory)) {
            throw new IllegalArgumentException(
                    "--teardown-ready-file must be outside the compact output directory");
        }
        if (Files.exists(marker)) {
            throw new IllegalStateException("teardown marker already exists: " + marker);
        }

        Path temporary = Files.createTempFile(
                parent, marker.getFileName().toString() + ".", ".tmp");
        try {
            Files.write(temporary, teardownMarkerToken().getBytes(StandardCharsets.UTF_8));
            try {
                Files.move(temporary, marker, StandardCopyOption.ATOMIC_MOVE);
            } catch (AtomicMoveNotSupportedException unsupported) {
                Files.move(temporary, marker);
            }
        } catch (IOException failure) {
            Files.deleteIfExists(temporary);
            throw failure;
        } catch (RuntimeException failure) {
            Files.deleteIfExists(temporary);
            throw failure;
        }
    }

    private String teardownMarkerToken() {
        return config.federationName + "\n" + config.workloadPlanSha256 + "\n";
    }

    private void destroyFederationWhenEmpty() throws Exception {
        long started = System.nanoTime();
        long deadline = started + config.timeoutMillis * 1_000_000L;
        boolean transportTimeoutObserved = false;
        while (true) {
            try {
                rti.destroyFederationExecution(config.federationName);
                metrics.metric("FM", "call_latency.destroy_federation_execution", "nanoseconds",
                        System.nanoTime() - started);
                return;
            } catch (FederationExecutionDoesNotExist alreadyDestroyed) {
                if (!config.allFederatesSynchronization || !transportTimeoutObserved) {
                    throw alreadyDestroyed;
                }
                metrics.metric("FM", "call_latency.destroy_federation_execution", "nanoseconds",
                        System.nanoTime() - started);
                return;
            } catch (FederatesCurrentlyJoined retry) {
                if (!config.allFederatesSynchronization || System.nanoTime() >= deadline) {
                    throw retry;
                }
                sleepBeforeLifecycleRetry(deadline, retry);
            } catch (RTIinternalError retry) {
                if (!config.allFederatesSynchronization || !isJgroupsDestroyTimeout(retry)
                        || System.nanoTime() >= deadline) {
                    throw retry;
                }
                transportTimeoutObserved = true;
                sleepBeforeLifecycleRetry(deadline, retry);
            }
        }
    }

    private static void sleepBeforeLifecycleRetry(long deadline, Exception retry)
            throws Exception {
        long remainingNanos = deadline - System.nanoTime();
        if (remainingNanos <= 0L) {
            throw retry;
        }
        long remainingMillis = Math.max(1L, remainingNanos / 1_000_000L);
        Thread.sleep(Math.min(10L, remainingMillis));
        if (System.nanoTime() >= deadline) {
            throw retry;
        }
    }

    private static boolean isJgroupsDestroyTimeout(Throwable failure) {
        Throwable current = failure;
        while (current != null) {
            if ("org.jgroups.TimeoutException".equals(current.getClass().getName())) {
                return true;
            }
            String message = current.getMessage();
            if (current == failure && message != null
                    && message.startsWith("Unknown exception received from RTI "
                            + "(class org.jgroups.TimeoutException) "
                            + "for destroyFederationExecution()")) {
                return true;
            }
            current = current.getCause();
        }
        return false;
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

    private void awaitAllFederatesPresent() throws Exception {
        awaitFederatePresent(PUBLISHER_NAME);
        for (int index = 1; index < config.participantCount; index++) {
            awaitFederatePresent(config.subscriberFederateName(index));
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
            synchronized (callbackSignal) {
                if (!condition.getAsBoolean()) {
                    callbackSignal.wait(1000L);
                }
            }
        }
        checkCallbackFailure();
    }

    private void signalCallback() {
        synchronized (callbackSignal) {
            callbackSignal.notifyAll();
        }
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
                && logicalTime == observation.logicalTime;
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
        if (config.compactSummary) {
            if ("updateAttributeValues".equals(operationName)) {
                updateAttributeDurations.add(Long.valueOf(elapsed));
            } else if ("sendInteraction".equals(operationName)) {
                sendInteractionDurations.add(Long.valueOf(elapsed));
            } else {
                throw new IllegalArgumentException(
                        "unsupported compact benchmark operation: " + operationName);
            }
            return;
        }
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
        signalCallback();
    }

    public void timeConstrainedEnabled(LogicalTime time) throws FederateInternalError {
        constrainedEnabled = true;
        signalCallback();
    }

    public void timeAdvanceGrant(LogicalTime time) throws FederateInternalError {
        try {
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
                            new IllegalStateException(
                                    "duplicate time advance grant evidence for batch "
                                            + deliveryBatch));
                    return;
                }
                if (callbackTime != expectedLogicalTime) {
                    callbackFailure.compareAndSet(null, new IllegalStateException(
                            "time advance grant " + callbackTime + " arrived for delivery batch "
                                    + deliveryBatch + ", expected " + expectedLogicalTime));
                    return;
                }
                if (!callbacksBeforeGrant && !config.allowGrantBeforeCallbacks) {
                    callbackFailure.compareAndSet(null, new IllegalStateException(
                            "time advance grant " + callbackTime
                                    + " arrived before delivery batch " + deliveryBatch
                                    + " completed"));
                    return;
                }
            }
            grantedTime = callbackTime;
            timeAdvanceGranted = true;
        } finally {
            signalCallback();
        }
    }

    public void synchronizationPointRegistrationSucceeded(String label)
            throws FederateInternalError {
        synchronizationRegistrationSucceeded = label;
        signalCallback();
    }

    public void synchronizationPointRegistrationFailed(String label,
            SynchronizationPointFailureReason reason) throws FederateInternalError {
        synchronizationFailure = "synchronization registration failed for " + label + ": " + reason;
        signalCallback();
    }

    public void announceSynchronizationPoint(String label, byte[] tag) throws FederateInternalError {
        if (DECLARATIONS_SYNC.equals(label)) {
            declarationsAnnounced = true;
        } else if (CONTROL_SYNC.equals(label)) {
            controlAnnounced = true;
        } else if (READY_SYNC.equals(label)) {
            readyAnnounced = true;
        } else if (MEASURE_SYNC.equals(label)) {
            measureAnnounced = true;
        } else if (START_SYNC.equals(label)) {
            startAnnounced = true;
        } else if (DONE_SYNC.equals(label)) {
            doneAnnounced = true;
        }
        signalCallback();
    }

    public void federationSynchronized(String label, FederateHandleSet failed)
            throws FederateInternalError {
        if (failed != null && !failed.isEmpty()) {
            String message = "federation synchronization failed for " + label
                    + ": " + failed.size() + " federate(s) did not synchronize";
            synchronizationFailure = message;
            callbackFailure.compareAndSet(null, new IllegalStateException(message));
            signalCallback();
            return;
        }
        if (DECLARATIONS_SYNC.equals(label)) {
            declarationsSynchronized = true;
        } else if (CONTROL_SYNC.equals(label)) {
            controlSynchronized = true;
        } else if (READY_SYNC.equals(label)) {
            readySynchronized = true;
        } else if (MEASURE_SYNC.equals(label)) {
            measureSynchronized = true;
        } else if (START_SYNC.equals(label)) {
            startSynchronized = true;
        } else if (DONE_SYNC.equals(label)) {
            doneSynchronized = true;
        }
        signalCallback();
    }

    public void discoverObjectInstance(ObjectInstanceHandle instance, ObjectClassHandle objectClass,
            String objectName) throws FederateInternalError {
        if (isCompactReceiveOrderWorkload()
                || (this.objectClass != null && this.objectClass.equals(objectClass))) {
            discoveredObject = instance;
            discoveredObjectClass = objectClass;
            discoveredObjectName = objectName;
            objectDiscovered = true;
        }
        signalCallback();
    }

    public void objectInstanceNameReservationSucceeded(String objectName)
            throws FederateInternalError {
        if (config.objectInstanceName.equals(objectName)) {
            nameReservationSucceeded = true;
            nameReservationComplete = true;
        }
        signalCallback();
    }

    public void objectInstanceNameReservationFailed(String objectName)
            throws FederateInternalError {
        if (config.objectInstanceName.equals(objectName)) {
            nameReservationSucceeded = false;
            nameReservationComplete = true;
        }
        signalCallback();
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
        if (config.receiveOrder && discoveredObject != null && discoveredObject.equals(instance)) {
            removedTime = -1L;
            objectRemoved = true;
        } else {
            callbackFailure.compareAndSet(null,
                    new IllegalStateException("object removal arrived without TSO logical time"));
        }
        signalCallback();
    }

    private void captureRemoval(ObjectInstanceHandle instance, LogicalTime time) {
        if (discoveredObject != null && discoveredObject.equals(instance)) {
            removedTime = logicalTime(time);
            objectRemoved = true;
        }
        signalCallback();
    }

    public void reflectAttributeValues(ObjectInstanceHandle instance, AttributeHandleValueMap attributes,
            byte[] tag, OrderType sentOrder, TransportationTypeHandle transport,
            SupplementalReflectInfo info) throws FederateInternalError {
        captureReflection(instance, attributes, null);
    }

    public void reflectAttributeValues(ObjectInstanceHandle instance, AttributeHandleValueMap attributes,
            byte[] tag, OrderType sentOrder, TransportationTypeHandle transport, LogicalTime time,
            OrderType receivedOrder, SupplementalReflectInfo info) throws FederateInternalError {
        captureReflection(instance, attributes, time);
    }

    public void reflectAttributeValues(ObjectInstanceHandle instance, AttributeHandleValueMap attributes,
            byte[] tag, OrderType sentOrder, TransportationTypeHandle transport, LogicalTime time,
            OrderType receivedOrder, MessageRetractionHandle retraction, SupplementalReflectInfo info)
            throws FederateInternalError {
        captureReflection(instance, attributes, time);
    }

    private void captureReflection(ObjectInstanceHandle instance,
            AttributeHandleValueMap attributes, LogicalTime time) {
        long callbackReceivedAtNanos = config.workloadPlan == null ? -1L : System.nanoTime();
        try {
            if (objectDiscovered
                    && discoveredObject != null
                    && discoveredObject.equals(instance)
                    && attributes.size() == 2
                    && attributes.containsKey(objectSequence)
                    && attributes.containsKey(objectPayload)) {
                int index = decodeInteger(attributes.get(objectSequence));
                if (index < 0 || index >= config.count + config.operationWarmup) {
                    invalidCallbacks.incrementAndGet();
                    return;
                }
                if (index < config.count && config.workloadPlan != null
                        && (receiveOrderBatchStartedNanos < 0L
                                || callbackReceivedAtNanos < receiveOrderBatchStartedNanos)) {
                    invalidCallbacks.incrementAndGet();
                    return;
                }
                String payload = decodeString(attributes.get(objectPayload));
                if (!callbackTimeMatches(index, time)
                        || !expectedPayload("attribute", index).equals(payload)) {
                    invalidCallbacks.incrementAndGet();
                    return;
                }
                Map<Integer, Observation> target = index < config.count
                        ? reflected : warmupReflected;
                Observation observation = new Observation(payload,
                        time == null ? -1L : logicalTime(time),
                        config.workloadPlan == null
                                ? System.nanoTime() : callbackReceivedAtNanos);
                if (config.compactSummary && index < config.count) {
                    acceptCompactCallback("attribute", target, observation, index, payload,
                            attributeCallbackDigest, attributeDigestLock,
                            nextExpectedAttributeIndex);
                } else {
                    if (target.putIfAbsent(index, observation) != null) {
                        duplicateCallbacks.incrementAndGet();
                    }
                }
            } else {
                invalidCallbacks.incrementAndGet();
            }
        } catch (Throwable failure) {
            callbackFailure.compareAndSet(null, failure);
        } finally {
            signalCallback();
        }
    }

    public void receiveInteraction(InteractionClassHandle interaction, ParameterHandleValueMap parameters,
            byte[] tag, OrderType sentOrder, TransportationTypeHandle transport,
            SupplementalReceiveInfo info) throws FederateInternalError {
        captureInteraction(interaction, parameters, null);
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
        long callbackReceivedAtNanos = config.workloadPlan == null ? -1L : System.nanoTime();
        try {
            if (subscriberReadyInteractionClass != null
                    && subscriberReadyInteractionClass.equals(interaction)) {
                captureSubscriberReady(parameters, time);
                return;
            }
            if (publisherAckInteractionClass != null
                    && publisherAckInteractionClass.equals(interaction)) {
                capturePublisherAcknowledgement(parameters, time);
                return;
            }
            if (interactionClass.equals(interaction)
                    && parameters.size() == 2
                    && parameters.containsKey(interactionSequence)
                    && parameters.containsKey(interactionPayload)) {
                int index = decodeInteger(parameters.get(interactionSequence));
                if (index < 0 || index >= config.count + config.operationWarmup) {
                    invalidCallbacks.incrementAndGet();
                    return;
                }
                if (index < config.count && config.workloadPlan != null
                        && (receiveOrderBatchStartedNanos < 0L
                                || callbackReceivedAtNanos < receiveOrderBatchStartedNanos)) {
                    invalidCallbacks.incrementAndGet();
                    return;
                }
                String payload = decodeString(parameters.get(interactionPayload));
                if (!callbackTimeMatches(index, time)
                        || !expectedPayload("interaction", index).equals(payload)) {
                    invalidCallbacks.incrementAndGet();
                    return;
                }
                Map<Integer, Observation> target = index < config.count
                        ? received : warmupReceived;
                Observation observation = new Observation(payload,
                        time == null ? -1L : logicalTime(time),
                        config.workloadPlan == null
                                ? System.nanoTime() : callbackReceivedAtNanos);
                if (config.compactSummary && index < config.count) {
                    acceptCompactCallback("interaction", target, observation, index, payload,
                            interactionCallbackDigest, interactionDigestLock,
                            nextExpectedInteractionIndex);
                } else {
                    if (target.putIfAbsent(index, observation) != null) {
                        duplicateCallbacks.incrementAndGet();
                    }
                }
            } else {
                invalidCallbacks.incrementAndGet();
            }
        } catch (Throwable failure) {
            callbackFailure.compareAndSet(null, failure);
        } finally {
            signalCallback();
        }
    }

    private void captureSubscriberReady(ParameterHandleValueMap parameters, LogicalTime time)
            throws DecoderException {
        if (time != null
                || parameters.size() != 1
                || !parameters.containsKey(subscriberReadyParticipantIndex)) {
            callbackFailure.compareAndSet(null,
                    new IllegalStateException("invalid subscriber-ready interaction"));
            return;
        }
        int participantIndex = decodeInteger(
                parameters.get(subscriberReadyParticipantIndex));
        if (participantIndex < 1 || participantIndex >= config.participantCount) {
            callbackFailure.compareAndSet(null,
                    new IllegalStateException(
                            "invalid subscriber-ready participant index: "
                                    + participantIndex));
            return;
        }
        if (!config.isPublisher()) {
            callbackFailure.compareAndSet(null,
                    new IllegalStateException(
                            "subscriber received subscriber-ready interaction"));
        } else {
            controlReadyParticipants.add(Integer.valueOf(participantIndex));
        }
    }

    private void capturePublisherAcknowledgement(ParameterHandleValueMap parameters,
            LogicalTime time) throws DecoderException {
        if (time != null
                || parameters.size() != 1
                || !parameters.containsKey(publisherAckParticipantIndex)) {
            callbackFailure.compareAndSet(null,
                    new IllegalStateException("invalid publisher-ack interaction"));
            return;
        }
        int participantIndex = decodeInteger(
                parameters.get(publisherAckParticipantIndex));
        if (participantIndex < 1 || participantIndex >= config.participantCount) {
            callbackFailure.compareAndSet(null,
                    new IllegalStateException(
                            "invalid publisher-ack participant index: "
                                    + participantIndex));
            return;
        }
        if (config.isPublisher()) {
            callbackFailure.compareAndSet(null,
                    new IllegalStateException(
                            "publisher received publisher-ack interaction"));
        } else if (participantIndex == config.participantIndex) {
            controlAcknowledged = true;
        }
    }

    private boolean callbackTimeMatches(int index, LogicalTime time) {
        return config.receiveOrder
                ? time == null
                : time != null && logicalTime(time) == index + 1L;
    }

    private String expectedPayload(String channel, int index) {
        if (index < config.count && config.workloadPlan != null) {
            PlanRecord record = config.workloadPlan.records.get(index);
            return "attribute".equals(channel)
                    ? record.attributePayload : record.interactionPayload;
        }
        return deterministicPayload(config.seed, channel, index);
    }

    private void acceptCompactCallback(String channel, Map<Integer, Observation> target,
            Observation observation, int index, String payload, MessageDigest digest,
            Object digestLock, AtomicInteger nextExpectedIndex) {
        synchronized (digestLock) {
            if (target.containsKey(Integer.valueOf(index))) {
                duplicateCallbacks.incrementAndGet();
                return;
            }
            int expectedIndex = nextExpectedIndex.get();
            if (index != expectedIndex) {
                invalidCallbacks.incrementAndGet();
                callbackFailure.compareAndSet(null, new IllegalStateException(
                        channel + " callback order violation: received index " + index
                                + ", expected " + expectedIndex));
                return;
            }
            if (!acceptCallbackTrace(channel, index, payload)) {
                return;
            }
            target.put(Integer.valueOf(index), observation);
            updateCallbackDigest(digest, index, payload);
            nextExpectedIndex.incrementAndGet();
        }
    }

    private boolean acceptCallbackTrace(String channel, int index, String payload) {
        synchronized (callbackTraceLock) {
            int ordinal = nextExpectedCallbackOrdinal.get();
            String expectedChannel = (ordinal & 1) == 0 ? "attribute" : "interaction";
            int expectedIndex = ordinal / 2;
            if (!expectedChannel.equals(channel) || expectedIndex != index) {
                invalidCallbacks.incrementAndGet();
                callbackFailure.compareAndSet(null, new IllegalStateException(
                        "callback trace violation: received " + channel + ":" + index
                                + ", expected " + expectedChannel + ":" + expectedIndex));
                return false;
            }
            updateCallbackTraceDigest(callbackTraceDigest, channel, index, payload);
            nextExpectedCallbackOrdinal.incrementAndGet();
            return true;
        }
    }

    private static void updateCallbackDigest(MessageDigest digest, int index, String payload) {
        if (index < 0 || !isLowercaseHexPayload(payload)) {
            throw new IllegalArgumentException("invalid callback digest input");
        }
        byte[] input = new byte[20];
        input[0] = (byte) (index >>> 24);
        input[1] = (byte) (index >>> 16);
        input[2] = (byte) (index >>> 8);
        input[3] = (byte) index;
        byte[] payloadBytes = payload.getBytes(StandardCharsets.US_ASCII);
        System.arraycopy(payloadBytes, 0, input, 4, payloadBytes.length);
        digest.update(input);
    }

    private static void updateCallbackTraceDigest(MessageDigest digest, String channel,
            int index, String payload) {
        if (index < 0 || !isLowercaseHexPayload(payload)) {
            throw new IllegalArgumentException("invalid callback trace input");
        }
        byte[] input = new byte[21];
        input[0] = (byte) ("attribute".equals(channel) ? 'A' : 'I');
        input[1] = (byte) (index >>> 24);
        input[2] = (byte) (index >>> 16);
        input[3] = (byte) (index >>> 8);
        input[4] = (byte) index;
        byte[] payloadBytes = payload.getBytes(StandardCharsets.US_ASCII);
        System.arraycopy(payloadBytes, 0, input, 5, payloadBytes.length);
        digest.update(input);
    }

    private static boolean isLowercaseHexPayload(String payload) {
        if (payload == null || payload.length() != 16) {
            return false;
        }
        for (int index = 0; index < payload.length(); index++) {
            char value = payload.charAt(index);
            if (!((value >= '0' && value <= '9') || (value >= 'a' && value <= 'f'))) {
                return false;
            }
        }
        return true;
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

    private static MessageDigest newSha256() {
        try {
            return MessageDigest.getInstance("SHA-256");
        } catch (NoSuchAlgorithmException impossible) {
            throw new IllegalStateException("SHA-256 is unavailable", impossible);
        }
    }

    private static String lowercaseHex(byte[] values) {
        final char[] digits = "0123456789abcdef".toCharArray();
        char[] result = new char[values.length * 2];
        for (int index = 0; index < values.length; index++) {
            int value = values[index] & 0xff;
            result[index * 2] = digits[value >>> 4];
            result[index * 2 + 1] = digits[value & 0x0f];
        }
        return new String(result);
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

    private static final class EncodedIteration {
        private final int index;
        private final long logicalTime;
        private final HLAfloat64Time timestamp;
        private final String objectValue;
        private final String interactionValue;
        private final AttributeHandleValueMap attributes;
        private final ParameterHandleValueMap parameters;

        private EncodedIteration(int index, long logicalTime, HLAfloat64Time timestamp,
                String objectValue, String interactionValue,
                AttributeHandleValueMap attributes, ParameterHandleValueMap parameters) {
            this.index = index;
            this.logicalTime = logicalTime;
            this.timestamp = timestamp;
            this.objectValue = objectValue;
            this.interactionValue = interactionValue;
            this.attributes = attributes;
            this.parameters = parameters;
        }
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

    private static final class PlanRecord {
        private final int index;
        private final long eventSequence;
        private final long targetOrdinal;
        private final long occurrenceOrdinal;
        private final String attributePayload;
        private final String interactionPayload;

        private PlanRecord(int index, long eventSequence, long targetOrdinal,
                long occurrenceOrdinal, String attributePayload, String interactionPayload) {
            this.index = index;
            this.eventSequence = eventSequence;
            this.targetOrdinal = targetOrdinal;
            this.occurrenceOrdinal = occurrenceOrdinal;
            this.attributePayload = attributePayload;
            this.interactionPayload = interactionPayload;
        }
    }

    private static final class WorkloadPlan {
        private static final byte[] MAGIC = new byte[]{
            'D', 'V', 'S', 'H', 'L', 'A', '1', 0
        };
        private static final int HEADER_SIZE = 8 + 4 + 8 + 32;
        private static final int RECORD_SIZE = 4 * 4 + 8 + 8;

        private final BigInteger seed;
        private final String planSha256;
        private final String topologySha256;
        private final List<PlanRecord> records;

        private WorkloadPlan(BigInteger seed, String planSha256, String topologySha256,
                List<PlanRecord> records) {
            this.seed = seed;
            this.planSha256 = planSha256;
            this.topologySha256 = topologySha256;
            this.records = Collections.unmodifiableList(records);
        }

        private static WorkloadPlan read(Path path, int expectedCount, BigInteger expectedSeed)
                throws IOException {
            byte[] bytes = Files.readAllBytes(path);
            if (bytes.length < HEADER_SIZE) {
                throw new IllegalArgumentException("workload plan is truncated before its header");
            }

            ByteBuffer input = ByteBuffer.wrap(bytes).order(ByteOrder.BIG_ENDIAN);
            byte[] magic = new byte[MAGIC.length];
            input.get(magic);
            if (!Arrays.equals(MAGIC, magic)) {
                throw new IllegalArgumentException("workload plan magic must be DVSHLA1\\0");
            }

            long declaredCount = unsignedInt(input.getInt());
            byte[] seedBytes = new byte[8];
            input.get(seedBytes);
            BigInteger declaredSeed = new BigInteger(1, seedBytes);
            byte[] topologyDigest = new byte[32];
            input.get(topologyDigest);

            if (declaredCount != expectedCount) {
                throw new IllegalArgumentException("workload plan count " + declaredCount
                        + " does not match --count " + expectedCount);
            }
            if (!declaredSeed.equals(expectedSeed)) {
                throw new IllegalArgumentException("workload plan seed " + declaredSeed
                        + " does not match --seed " + expectedSeed);
            }

            long requiredLength = HEADER_SIZE + declaredCount * (long) RECORD_SIZE;
            if (bytes.length < requiredLength) {
                throw new IllegalArgumentException("workload plan is truncated: expected "
                        + requiredLength + " bytes, found " + bytes.length);
            }
            if (bytes.length > requiredLength) {
                throw new IllegalArgumentException("workload plan has trailing bytes: expected "
                        + requiredLength + " bytes, found " + bytes.length);
            }

            List<PlanRecord> records = new ArrayList<PlanRecord>(expectedCount);
            Set<Long> seenIndices = new HashSet<Long>();
            PlanRecord previous = null;
            for (int ordinal = 0; ordinal < expectedCount; ordinal++) {
                long index = unsignedInt(input.getInt());
                long eventSequence = unsignedInt(input.getInt());
                long targetOrdinal = unsignedInt(input.getInt());
                long occurrenceOrdinal = unsignedInt(input.getInt());
                byte[] attributePayload = new byte[8];
                byte[] interactionPayload = new byte[8];
                input.get(attributePayload);
                input.get(interactionPayload);

                if (!seenIndices.add(Long.valueOf(index))) {
                    throw new IllegalArgumentException(
                            "workload plan contains duplicate index " + index);
                }
                if (index != ordinal) {
                    throw new IllegalArgumentException("workload plan index " + index
                            + " is out of order; expected " + ordinal);
                }
                String attributeHex = lowercaseHex(attributePayload);
                String interactionHex = lowercaseHex(interactionPayload);
                if (!isLowercaseHexPayload(attributeHex)
                        || !isLowercaseHexPayload(interactionHex)) {
                    throw new IllegalArgumentException("workload plan payload encoding failed");
                }
                PlanRecord record = new PlanRecord((int) index, eventSequence, targetOrdinal,
                        occurrenceOrdinal, attributeHex, interactionHex);
                if (previous != null && !recordFollows(previous, record)) {
                    throw new IllegalArgumentException("workload plan record " + index
                            + " has a non-increasing event/target/occurrence tuple");
                }
                records.add(record);
                previous = record;
            }
            if (input.hasRemaining() || records.size() != expectedCount) {
                throw new IllegalArgumentException("workload plan record validation failed");
            }

            String planDigest = lowercaseHex(newSha256().digest(bytes));
            String topologySha256 = lowercaseHex(topologyDigest);
            if (planDigest.length() != 64 || topologySha256.length() != 64) {
                throw new IllegalStateException("workload plan digest validation failed");
            }
            return new WorkloadPlan(declaredSeed, planDigest, topologySha256, records);
        }

        private static long unsignedInt(int value) {
            return value & 0xffffffffL;
        }

        private static boolean recordFollows(PlanRecord previous, PlanRecord current) {
            if (current.eventSequence != previous.eventSequence) {
                return current.eventSequence > previous.eventSequence;
            }
            if (current.targetOrdinal != previous.targetOrdinal) {
                return current.targetOrdinal > previous.targetOrdinal;
            }
            return current.occurrenceOrdinal > previous.occurrenceOrdinal;
        }
    }

    private static final class Config {
        private final String role;
        private final String seed;
        private final BigInteger numericSeed;
        private final int count;
        private final int operationWarmup;
        private final String localSettingsDesignator;
        private final boolean allFederatesSynchronization;
        private final boolean receiveOrder;
        private final boolean allowGrantBeforeCallbacks;
        private final boolean tmAdvanceOnly;
        private final String federationName;
        private final Path fom;
        private final Path outputDirectory;
        private final long timeoutMillis;
        private final boolean compactSummary;
        private final String workloadPlanSha256;
        private final WorkloadPlan workloadPlan;
        private final Path joinReadyFile;
        private final Path startupReadyFile;
        private final Path startupReleaseFile;
        private final Path teardownReadyFile;
        private final int participantCount;
        private final int participantIndex;
        private final String objectClass;
        private final String interactionClass;
        private final String objectInstanceName;

        private Config(Map<String, String> args) throws IOException {
            role = required(args, "role").toLowerCase();
            if (!Arrays.asList("publisher", "subscriber").contains(role)) {
                throw new IllegalArgumentException("--role must be publisher or subscriber");
            }
            seed = value(args, "seed", "1516");
            participantCount = Integer.parseInt(value(args, "participant-count", "2"));
            int defaultParticipantIndex = isPublisher() ? 0 : 1;
            participantIndex = Integer.parseInt(value(
                    args, "participant-index", Integer.toString(defaultParticipantIndex)));
            if (participantCount < 2
                    || (isPublisher() && participantIndex != 0)
                    || (!isPublisher()
                            && (participantIndex < 1 || participantIndex >= participantCount))) {
                throw new IllegalArgumentException(
                        "--participant-count/index is outside the role range");
            }
            count = Integer.parseInt(value(args, "count", "100"));
            operationWarmup = Integer.parseInt(value(args, "operation-warmup", "0"));
            localSettingsDesignator = value(args, "local-settings-designator", "");
            allFederatesSynchronization = Boolean.parseBoolean(
                    value(args, "all-federates-sync", "false"));
            receiveOrder = Boolean.parseBoolean(value(args, "receive-order", "false"));
            allowGrantBeforeCallbacks = Boolean.parseBoolean(
                    value(args, "allow-grant-before-callbacks", "false"));
            tmAdvanceOnly = Boolean.parseBoolean(value(args, "tm-advance-only", "false"));
            compactSummary = Boolean.parseBoolean(value(args, "compact-summary", "false"));
            federationName = value(args, "federation", "GortiCommercialRtiVerifier-" + seed);
            fom = Paths.get(required(args, "fom")).toAbsolutePath().normalize();
            outputDirectory = Paths.get(required(args, "output")).toAbsolutePath().normalize();
            String joinReadyFileValue = args.get("join-ready-file");
            joinReadyFile = joinReadyFileValue == null ? null
                    : Paths.get(joinReadyFileValue).toAbsolutePath().normalize();
            String startupReadyFileValue = args.get("startup-ready-file");
            startupReadyFile = startupReadyFileValue == null ? null
                    : Paths.get(startupReadyFileValue).toAbsolutePath().normalize();
            String startupReleaseFileValue = args.get("startup-release-file");
            startupReleaseFile = startupReleaseFileValue == null ? null
                    : Paths.get(startupReleaseFileValue).toAbsolutePath().normalize();
            String teardownReadyFileValue = args.get("teardown-ready-file");
            teardownReadyFile = teardownReadyFileValue == null ? null
                    : Paths.get(teardownReadyFileValue).toAbsolutePath().normalize();
            timeoutMillis = Long.parseLong(value(args, "timeout-ms", "30000"));
            objectClass = value(args, "object-class", DEFAULT_OBJECT_CLASS);
            interactionClass = value(args, "interaction-class", DEFAULT_INTERACTION_CLASS);
            objectInstanceName = value(args, "object-name", DEFAULT_OBJECT_INSTANCE_NAME);
            if (objectClass == null || objectClass.isEmpty()) {
                throw new IllegalArgumentException("--object-class must not be empty");
            }
            if (interactionClass == null || interactionClass.isEmpty()) {
                throw new IllegalArgumentException("--interaction-class must not be empty");
            }
            if (objectInstanceName == null || objectInstanceName.isEmpty()) {
                throw new IllegalArgumentException("--object-name must not be empty");
            }
            if (count <= 0 || timeoutMillis <= 0) {
                throw new IllegalArgumentException("--count and --timeout-ms must be positive");
            }
            if (operationWarmup < 0) {
                throw new IllegalArgumentException("--operation-warmup must not be negative");
            }
            if (operationWarmup > 0 && !receiveOrder) {
                throw new IllegalArgumentException("--operation-warmup requires --receive-order");
            }
            if (tmAdvanceOnly && receiveOrder) {
                throw new IllegalArgumentException("--tm-advance-only forbids receive-order");
            }
            if (count > Integer.MAX_VALUE - operationWarmup) {
                throw new IllegalArgumentException("count plus operation warmup exceeds sequence range");
            }
            if (!Files.isRegularFile(fom)) {
                throw new IllegalArgumentException("FOM does not exist: " + fom);
            }
            String workloadPlanValue = args.get("workload-plan");
            workloadPlanSha256 = args.get("workload-plan-sha256");
            if (workloadPlanValue != null && workloadPlanValue.isEmpty()) {
                throw new IllegalArgumentException("--workload-plan must not be empty");
            }
            if (workloadPlanValue != null && !receiveOrder) {
                throw new IllegalArgumentException(
                        "--workload-plan requires --receive-order true");
            }
            if (compactSummary && workloadPlanValue == null) {
                throw new IllegalArgumentException(
                        "--compact-summary true requires --workload-plan");
            }
            if (workloadPlanSha256 != null && workloadPlanValue == null) {
                throw new IllegalArgumentException(
                        "--workload-plan-sha256 requires --workload-plan");
            }
            if (workloadPlanSha256 != null && !isLowercaseSha256(workloadPlanSha256)) {
                throw new IllegalArgumentException(
                        "--workload-plan-sha256 must be a 64-character lowercase SHA-256");
            }
            if (compactSummary && workloadPlanSha256 == null) {
                throw new IllegalArgumentException(
                        "--compact-summary true requires --workload-plan-sha256");
            }
            if (joinReadyFileValue != null && joinReadyFileValue.isEmpty()) {
                throw new IllegalArgumentException("--join-ready-file must not be empty");
            }
            if (joinReadyFile != null
                    && (!compactSummary || !receiveOrder || workloadPlanValue == null)) {
                throw new IllegalArgumentException("--join-ready-file is valid only in "
                        + "compact receive-order DVSHLA1 mode");
            }
            if (joinReadyFile != null && joinReadyFile.startsWith(outputDirectory)) {
                throw new IllegalArgumentException(
                        "--join-ready-file must be outside the compact output directory");
            }
            if (startupReadyFileValue != null && startupReadyFileValue.isEmpty()) {
                throw new IllegalArgumentException("--startup-ready-file must not be empty");
            }
            if (startupReadyFile != null
                    && (!compactSummary || !receiveOrder || workloadPlanValue == null)) {
                throw new IllegalArgumentException("--startup-ready-file is valid only in "
                        + "compact receive-order DVSHLA1 mode");
            }
            if (startupReadyFile != null
                    && startupReadyFile.startsWith(outputDirectory)) {
                throw new IllegalArgumentException(
                        "--startup-ready-file must be outside the compact output directory");
            }
            if (startupReleaseFileValue != null && startupReleaseFileValue.isEmpty()) {
                throw new IllegalArgumentException("--startup-release-file must not be empty");
            }
            if (startupReleaseFile != null
                    && (!isPublisher() || startupReadyFile == null || !compactSummary
                            || !receiveOrder || workloadPlanValue == null)) {
                throw new IllegalArgumentException("--startup-release-file is valid only for a "
                        + "publisher with startup readiness in compact receive-order DVSHLA1 mode");
            }
            if (startupReleaseFile != null
                    && startupReleaseFile.startsWith(outputDirectory)) {
                throw new IllegalArgumentException(
                        "--startup-release-file must be outside the compact output directory");
            }
            if (teardownReadyFileValue != null && teardownReadyFileValue.isEmpty()) {
                throw new IllegalArgumentException("--teardown-ready-file must not be empty");
            }
            if (teardownReadyFile != null && !allFederatesSynchronization) {
                throw new IllegalArgumentException("--teardown-ready-file is valid only in "
                        + "all-federates mode");
            }
            if (teardownReadyFile != null
                    && teardownReadyFile.startsWith(outputDirectory)) {
                throw new IllegalArgumentException(
                        "--teardown-ready-file must be outside the compact output directory");
            }
            numericSeed = workloadPlanValue != null || compactSummary
                    ? parseUnsignedSeed(seed) : null;
            if (workloadPlanValue == null) {
                workloadPlan = null;
            } else {
                Path planPath = Paths.get(workloadPlanValue).toAbsolutePath().normalize();
                if (!Files.isRegularFile(planPath)) {
                    throw new IllegalArgumentException("workload plan does not exist: " + planPath);
                }
                WorkloadPlan loadedPlan = WorkloadPlan.read(planPath, count, numericSeed);
                if (workloadPlanSha256 != null
                        && !workloadPlanSha256.equals(loadedPlan.planSha256)) {
                    throw new IllegalArgumentException("workload plan SHA-256 mismatch: actual="
                            + loadedPlan.planSha256 + " expected=" + workloadPlanSha256);
                }
                workloadPlan = loadedPlan;
            }
        }

        private static Config parse(String[] commandLine) throws IOException {
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

        private String federateName() {
            return isPublisher() ? PUBLISHER_NAME : subscriberFederateName(participantIndex);
        }

        private String subscriberFederateName(int index) {
            return participantCount == 2 ? SUBSCRIBER_NAME : SUBSCRIBER_NAME + "-" + index;
        }

        private boolean registersSynchronization(String label) {
            if (participantCount > 2) {
                return isPublisher();
            }
            return READY_SYNC.equals(label) ? !isPublisher() : isPublisher();
        }

        private Path teardownSibling(String fileName) {
            if (teardownReadyFile == null) {
                throw new IllegalStateException("teardown marker base is not configured");
            }
            return teardownReadyFile.resolveSibling(fileName);
        }

        private Path teardownReadyMarker(int subscriberIndex) {
            if (participantCount == 2) {
                return teardownReadyFile;
            }
            return teardownSibling(".portico-subscriber-" + subscriberIndex
                    + "-resigned.ready");
        }

        private Path publisherResignedMarker(int subscriberIndex) {
            if (participantCount == 2) {
                return teardownSibling(PUBLISHER_RESIGNED_MARKER);
            }
            return teardownSibling(".portico-publisher-resigned-for-subscriber-"
                    + subscriberIndex + ".ready");
        }

        private Path subscriberDisconnectedMarker(int subscriberIndex) {
            if (participantCount == 2) {
                return teardownSibling(SUBSCRIBER_DISCONNECTED_MARKER);
            }
            return teardownSibling(".portico-subscriber-" + subscriberIndex
                    + "-disconnected.ready");
        }

        private Path summaryPath() {
            return outputDirectory.resolve(role + "-summary.json");
        }

        private Path summaryTemporaryPath() {
            return outputDirectory.resolve("." + role + "-summary.json.tmp");
        }

        private static BigInteger parseUnsignedSeed(String value) {
            final BigInteger result;
            try {
                result = new BigInteger(value);
            } catch (NumberFormatException failure) {
                throw new IllegalArgumentException(
                        "--seed must be an unsigned 64-bit integer in workload plan mode", failure);
            }
            if (result.signum() < 0 || result.bitLength() > 64) {
                throw new IllegalArgumentException(
                        "--seed must be an unsigned 64-bit integer in workload plan mode");
            }
            return result;
        }

        private static boolean isLowercaseSha256(String value) {
            if (value.length() != 64) {
                return false;
            }
            for (int index = 0; index < value.length(); index++) {
                char character = value.charAt(index);
                if (!((character >= '0' && character <= '9')
                        || (character >= 'a' && character <= 'f'))) {
                    return false;
                }
            }
            return true;
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
            this.writer = path == null ? null
                    : Files.newBufferedWriter(path, StandardCharsets.UTF_8);
            this.actor = actor;
        }

        private synchronized void event(String service, String event, Map<String, Object> data)
                throws IOException {
            if (writer == null) {
                return;
            }
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
            if (writer != null) {
                writer.close();
            }
        }
    }

    private static final class MetricLogger implements Closeable {
        private final BufferedWriter writer;

        private MetricLogger(Path path) throws IOException {
            writer = path == null ? null : Files.newBufferedWriter(path, StandardCharsets.UTF_8);
        }

        private synchronized void metric(String service, String metric, String unit, Number value) {
            if (writer == null) {
                return;
            }
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
            if (writer != null) {
                writer.close();
            }
        }
    }

    private static final class SampleLogger implements Closeable {
        private final BufferedWriter writer;
        private long sequence;

        private SampleLogger(Path path) throws IOException {
            writer = path == null ? null : Files.newBufferedWriter(path, StandardCharsets.UTF_8);
        }

        private synchronized void sample(String operation, long durationNanos, String service,
                String sampleKind) {
            if (writer == null) {
                return;
            }
            writeSample(operation, durationNanos, data(
                    "sample_kind", sampleKind,
                    "service", service));
        }

        private synchronized void grantBoundarySample(long durationNanos) {
            if (writer == null) {
                return;
            }
            writeSample("timeAdvanceGrantLatency", durationNanos, data(
                    "boundary", "grant",
                    "service", "TM"));
        }

        private void writeSample(String operation, long durationNanos,
                Map<String, Object> dimensions) {
            if (durationNanos < 0L) {
                throw new IllegalArgumentException("sample duration must be non-negative");
            }
            try {
                validateService(String.valueOf(dimensions.get("service")));
                writer.write("{\"sequence\":" + sequence++
                        + ",\"operation\":" + Json.string(operation)
                        + ",\"duration_ns\":" + durationNanos
                        + ",\"dimensions\":" + Json.object(dimensions) + "}");
                writer.newLine();
                writer.flush();
            } catch (IOException failure) {
                throw new IllegalStateException("unable to write raw sample", failure);
            }
        }

        public void close() throws IOException {
            if (writer != null) {
                writer.close();
            }
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
