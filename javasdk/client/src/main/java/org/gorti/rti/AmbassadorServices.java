package org.gorti.rti;

import com.google.protobuf.ByteString;
import hla.rti1516e.*;
import hla.rti1516e.exceptions.*;
import io.grpc.StatusRuntimeException;
import io.grpc.Channel;
import java.io.IOException;
import java.net.URI;
import java.net.URL;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.HashSet;
import java.util.HashMap;
import java.util.ArrayList;
import java.util.List;
import java.util.ArrayDeque;
import java.util.Map;
import java.util.Set;
import java.util.UUID;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicLong;
import java.util.function.Supplier;
import org.gorti.rti.transport.WireSession;
import org.gorti.rti.types.Handles;
import org.gorti.rti.types.HandleCollections;
import org.gorti.rti.types.TimeValues;
import rti.v1.Common;
import rti.v1.Federation;
import rti.v1.FederationServiceGrpc;
import rti.v1.Stream;
import rti.v1.Support;
import rti.v1.SupportServiceGrpc;
import rti.v1.Time;
import rti.v1.TimeServiceGrpc;

/** Service state is serialized by GortiRtiAmbassador; stream readers only enqueue. */
final class AmbassadorServices implements ServiceContext {
    private static final Common.WireVersion V1 = Common.WireVersion.WIRE_VERSION_V1;
    private static final String FLOAT64_TIME = "HLAfloat64Time";
    private final ArrayBlockingQueue<Callback> callbacks = new ArrayBlockingQueue<>(16384);
    private final ArrayDeque<Stream.FederateEvent> restoredControlCallbacks = new ArrayDeque<>();
    private final ArrayDeque<LocalNotification> restoredLocalNotifications = new ArrayDeque<>();
    private long nextLocalOrder;
    private boolean restoredControlsReady;
    private final Set<ByteString> transportationAdmissionRetries = new HashSet<>();
    private WireSession session;
    private FederateAmbassador ambassador;
    private String federation;
    private long handle;
    private long federationGeneration;
    final ExchangeServices exchange = new ExchangeServices(this);
    final SynchronizationServices synchronization = new SynchronizationServices(this);
    private final CallbackSink responseCallbacks = new CallbackSink() {
        @Override public void enqueue(Invocation callback) { capture().enqueue(callback); }
        @Override public CallbackSink capture() {
            long joinedEpoch = epoch;
            long restoredEpoch = callbackEpoch;
            return callback -> enqueueFederateCallback(callback, joinedEpoch, restoredEpoch);
        }
    };
    final OwnershipServices ownership = new OwnershipServices(this, responseCallbacks, exchange);
    final DdmServices ddm = new DdmServices(this, exchange);
    final SupportServices support = new SupportServices(this, exchange);
    private final OwnershipCallbackDelivery ownershipDelivery = new OwnershipCallbackDelivery(this);
    private boolean callbackEntrySupported;
    private final CallbackInvocationDelivery callbackInvocation = new CallbackInvocationDelivery(this::reportGenericCallback, () -> callbackEntrySupported);
    final TransportationServices transportation = new TransportationServices(this, responseCallbacks, exchange);
    final SaveRestoreServices saveRestore = new SaveRestoreServices(this, responseCallbacks, this::checkpointBytes, this::restoreReady);
    private final Map<String, RuntimeCheckpoint> checkpoints = new HashMap<>();
    private String pendingSaveLabel;
    private RuntimeCheckpoint pendingCheckpoint;
    private RuntimeCheckpoint pendingRestoreCheckpoint;
    private String pendingRestoreLabel;
    private long pendingRestoreHandle;
    private RTIinternalError pendingRestoreFailure;
    private boolean pendingRestoreValidated;
    private volatile long epoch;
    private long connectionEpoch;
    private volatile long callbackEpoch;
    private long appliedCallbackEpoch;
    private long hydratedCallbackEpoch;
    private final Object restoreHandoff = new Object();
    private RestoreHydration restoreHydration;
    private final AtomicLong restoreGate = new AtomicLong();
    private volatile Throwable streamFailure;
    private boolean callbacksEnabled = true;
    private volatile String callbackSaveLabel;
    private volatile String admittedSaveLabel;
    private volatile boolean callbackRestoreInProgress;
    private boolean inCallback;
    private boolean regulationPending;
    private boolean constrainedPending;
    private boolean advancePending;
    private boolean regulating;
    private boolean constrained;
    private LogicalTime logicalTime = TimeValues.time(0);
    private LogicalTimeFactory joinedTimeFactory;
    private RTIinternalError pendingJoinCleanup;

    public void connect(FederateAmbassador target, CallbackModel model) throws RTIexception {
        connect(target, model, System.getProperty("gorti.endpoint", "grpcs://localhost:50051"));
    }

    public void connect(FederateAmbassador target, CallbackModel model, String settings) throws RTIexception {
        outsideCallback();
        if (session != null) throw new AlreadyConnected("This ambassador is already connected");
        if (model != CallbackModel.HLA_EVOKED) throw new UnsupportedCallbackModel("Java foundation supports HLA_EVOKED");
        if (target == null) throw new ConnectionFailed("Federate ambassador is null");
        URI endpoint;
        try {
            endpoint = URI.create(settings);
            if (!("grpc".equals(endpoint.getScheme()) || "grpcs".equals(endpoint.getScheme()))
                    || endpoint.getHost() == null || endpoint.getPort() < 1 || endpoint.getPort() > 65535
                    || endpoint.getUserInfo() != null || endpoint.getQuery() != null || endpoint.getFragment() != null
                    || !(endpoint.getPath() == null || endpoint.getPath().isEmpty())) {
                throw new IllegalArgumentException("Expected grpc://host:port or grpcs://host:port");
            }
        } catch (RuntimeException failure) {
            throw new InvalidLocalSettingsDesignator("Expected grpc://host:port or grpcs://host:port", failure);
        }
        WireSession candidate = new WireSession(endpoint.getRawAuthority(), "grpc".equals(endpoint.getScheme()));
        try {
            FederationServiceGrpc.newBlockingStub(candidate.unauthenticatedChannel()).listFederations(
                    Federation.ListFederationsRequest.newBuilder().setWireVersion(V1).build());
        } catch (RuntimeException failure) {
            try { candidate.close(); }
            catch (RuntimeException cleanup) { failure.addSuppressed(cleanup); }
            if (failure instanceof StatusRuntimeException) {
                io.grpc.Status.Code code = ((StatusRuntimeException) failure).getStatus().getCode();
                if (code != io.grpc.Status.Code.INTERNAL && code != io.grpc.Status.Code.DATA_LOSS) {
                    throw new ConnectionFailed("Cannot connect to " + endpoint, failure);
                }
            }
            throw new RTIinternalError("RTI connection initialization failed", failure);
        }
        session = candidate;
        ambassador = target;
        streamFailure = null;
        callbacksEnabled = true;
        callbacks.clear();
        restoredLocalNotifications.clear();
        transportationAdmissionRetries.clear();
        epoch++;
        connectionEpoch++;
    }

    public void disconnect() throws RTIexception {
        outsideCallback();
        if (handle != 0) throw new FederateIsExecutionMember("Resign before disconnecting");
        try {
            if (session != null) session.close();
        } catch (RuntimeException failure) {
            throw new RTIinternalError("Disconnected, but transport teardown failed", failure);
        } finally {
            session = null;
            ambassador = null;
            epoch++;
            connectionEpoch++;
            callbacks.clear();
            transportationAdmissionRetries.clear();
            streamFailure = null;
        }
    }

    public void createFederationExecution(String name, URL module) throws RTIexception {
        createFederationExecution(name, new URL[]{module});
    }
    public void createFederationExecution(String name, URL[] modules) throws RTIexception {
        createFederationExecution(name, modules, null, "HLAfloat64Time");
    }
    public void createFederationExecution(String name, URL[] modules, String timeName) throws RTIexception {
        createFederationExecution(name, modules, null, timeName);
    }
    public void createFederationExecution(String name, URL[] modules, URL mim) throws RTIexception {
        createFederationExecution(name, modules, mim, "HLAfloat64Time");
    }
    public void createFederationExecution(String name, URL[] modules, URL mim, String timeName) throws RTIexception {
        connected();
        selectTimeFactory(timeName);
        Federation.CreateFederationRequest.Builder request = Federation.CreateFederationRequest.newBuilder()
                .setWireVersion(mim == null ? V1 : Common.WireVersion.WIRE_VERSION_V2)
                .setFederationName(name).setMode(Common.Mode.MODE_BEST_EFFORT)
                .setLogicalTimeImplementationName(timeName.isEmpty() ? FLOAT64_TIME : timeName);
        if (modules == null || modules.length == 0) {
            throw new CouldNotOpenFDD("Creation requires at least one FOM module");
        }
        for (URL module : modules) request.addFomModules(readModule(module, false));
        if (mim != null) request.setMimModule(readModule(mim, true));
        Federation.CreateFederationResponse created = rpc(() -> federationStub().createFederation(request.build()));
        String selected = timeImplementationName(created.getLogicalTimeImplementationName());
        if (!timeImplementationName(timeName).equals(selected)) {
            throw new RTIinternalError("Server created federation " + name
                    + " with unexpected logical time implementation " + selected + "; expected " + timeImplementationName(timeName));
        }
    }

    private Common.FOMModule readModule(URL url, boolean mim) throws RTIexception {
        try {
            if (url == null || !"file".equals(url.getProtocol())) {
                throw new IOException("This Java profile accepts local file URLs only");
            }
            Path path = Path.of(url.toURI());
            if (Files.size(path) > 4 * 1024 * 1024) throw new IOException("Module exceeds 4 MiB");
            return Common.FOMModule.newBuilder().setPath(url.toExternalForm())
                    .setXml(ByteString.copyFrom(Files.readAllBytes(path))).build();
        } catch (Exception failure) {
            if (mim) throw new CouldNotOpenMIM("Cannot read selected MIM: " + url, failure);
            throw new CouldNotOpenFDD("Cannot read selected FOM: " + url, failure);
        }
    }

    public void destroyFederationExecution(String name) throws RTIexception {
        connected();
        long generation = generation(name);
        rpc(() -> federationStub().destroyFederation(Federation.DestroyFederationRequest.newBuilder()
                .setWireVersion(V1).setFederationName(name).setExpectedFederationGeneration(generation).build()));
    }

    public void listFederationExecutions() throws RTIexception {
        connected();
        InformationSet result = new InformationSet();
        for (Federation.FederationSummary entry : list().getFederationsList()) {
            result.add(new FederationExecutionInformation(entry.getName(),
                    timeImplementationName(entry.getLogicalTimeImplementationName())));
        }
        enqueueFederateCallback(LocalNotification.executions(result), epoch, callbackEpoch);
    }

    public FederateHandle joinFederationExecution(String type, String name) throws RTIexception {
        return joinFederationExecution("java-" + UUID.randomUUID(), type, name, new URL[0]);
    }
    public FederateHandle joinFederationExecution(String type, String name, URL[] modules) throws RTIexception {
        return joinFederationExecution("java-" + UUID.randomUUID(), type, name, modules);
    }
    public FederateHandle joinFederationExecution(String federateName, String type, String name) throws RTIexception {
        return joinFederationExecution(federateName, type, name, new URL[0]);
    }
    public FederateHandle joinFederationExecution(String federateName, String type, String name, URL[] modules)
            throws RTIexception {
        outsideCallback();
        connected();
        if (handle != 0) throw new FederateAlreadyExecutionMember("Already joined");
        Federation.FederationSummary selected = federationSummary(name);
        selectTimeFactory(selected.getLogicalTimeImplementationName());
        long generation = selected.getFederationGeneration();
        Federation.JoinFederationRequest.Builder request = Federation.JoinFederationRequest.newBuilder()
                .setWireVersion(V1).setFederationName(name).setFederateName(federateName).setFederateType(type)
                .setExpectedFederationGeneration(generation);
        if (modules == null) throw new CouldNotOpenFDD("Additional module array is null");
        for (URL module : modules) request.addAdditionalFomModules(readModule(module, false));
        Federation.JoinFederationResponse joined;
        try {
            joined = rpc(() -> federationStub().joinFederation(request.build()));
        } catch (FederateAlreadyExecutionMember conflict) {
            throw new FederateNameAlreadyInUse("Federate name is already joined: " + federateName, conflict);
        }
        federation = name;
        federationGeneration = generation;
        handle = joined.getFederateHandle();
        session.setMembership(name, handle, generation, joined.getSessionToken());
        long current = ++epoch;
        resetTime();
        callbackEntrySupported = joined.getCallbackInvocationEntrySupported();
        streamFailure = null;
        try {
            joinedTimeFactory = selectTimeFactory(joined.getLogicalTimeImplementationName());
            logicalTime = joinedTimeFactory.makeInitial();
        } catch (CouldNotCreateLogicalTimeFactory failure) {
            try {
                resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST);
            } catch (RTIexception cleanup) {
                failure.addSuppressed(cleanup);
                // Keep the authenticated identity for a retry, but allow no time use.
                pendingJoinCleanup = new RTIinternalError("Unsupported logical time factory; compensating resignation failed. Retry resignation.", failure);
                throw pendingJoinCleanup;
            }
            throw failure;
        }
        try {
            if (joined.getTransportMode() == Federation.TransportMode.TRANSPORT_MODE_DDS) {
                throw new IllegalStateException("Java foundation supports the gRPC data plane only");
            }
            if (generation != 0) ownershipDelivery.enableIfSupported();
            session.subscribe(event -> {
                if (epoch != current) return;
                if (!CallbackInvocationDelivery.isOutcomeOnly(event)) ownershipDelivery.observe(event);
                boolean historic = CallbackInvocationDelivery.isRecovered(event);
                if (!historic) observeCallbackPhase(event);
                if (!historic && event.hasRestoreBegun()) restoreGate.updateAndGet(value -> (value + 1L) | 1L);
                List<Callback> connectionReports = !historic && event.hasRestoreCompleted() ? new ArrayList<>() : List.of();
                RestoreHydration completion = null;
                if (!historic && event.hasRestoreCompleted()) {
                    Stream.FederationRestored restored = event.getRestoreCompleted();
                    if (!restored.hasFederateHandle() || restored.getFederateHandle() == 0
                            || !restored.hasCallbackEpoch()
                            || Long.compareUnsigned(restored.getCallbackEpoch(), callbackEpoch) <= 0) {
                        streamFailure = new IllegalStateException("Invalid restored membership or callback epoch");
                        callbacks.offer(() -> { });
                        return;
                    }
                    // Retain only internal bookkeeping from obsolete callbacks. A legal
                    // support query may hydrate the state before the terminal is evoked.
                    List<Callback> obsolete = new ArrayList<>();
                    callbacks.drainTo(obsolete);
                    obsolete.removeIf(callback -> {
                        if (!callback.connectionScoped()) return false;
                        connectionReports.add(callback);
                        return true;
                    });
                    completion = new RestoreHydration(current, restored, obsolete);
                    synchronized (restoreHandoff) {
                        if (epoch != current) return;
                        restoreHydration = completion;
                        callbackEpoch = restored.getCallbackEpoch();
                        ownershipDelivery.restoreEpoch();
                        callbackInvocation.restoreEpoch();
                    }
                }
                long admittedEpoch = callbackEpoch;
                long admittedRestoreGate = restoreGate.get();
                RestoreHydration admittedHydration = completion;
                enqueue(new Callback() {
                    @Override public void invoke() throws RTIexception {
                        if (epoch != current) return;
                        if (admittedEpoch != callbackEpoch) {
                            retire();
                            return;
                        }
                        deliver(event, admittedRestoreGate);
                    }
                    @Override public void retire() {
                        if (!historic) saveTerminal(event);
                        if (admittedHydration != null) admittedHydration.retire();
                    }
                    @Override public Stream.FederateEvent checkpointEvent() { return event; }
                });
                for (Callback report : connectionReports) enqueue(report);
            }, failure -> {
                if (epoch == current) {
                    streamFailure = failure;
                    callbacks.offer(() -> { });
                }
            });
        } catch (RuntimeException | RTIexception failure) {
            try { resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST); }
            catch (RTIexception cleanup) { failure.addSuppressed(cleanup); }
            throw new RTIinternalError("Callback subscription failed after join", failure);
        }
        return Handles.federate(handle);
    }

    public void resignFederationExecution(ResignAction action) throws RTIexception {
        outsideCallback();
        joined();
        if (streamFailure == null && pendingJoinCleanup == null) memberTransportReady();
        if (action == null) throw new InvalidResignAction("Resign action is null");
        Common.ResignAction wire;
        switch (action) {
            case UNCONDITIONALLY_DIVEST_ATTRIBUTES: wire = Common.ResignAction.RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES; break;
            case DELETE_OBJECTS: wire = Common.ResignAction.RESIGN_ACTION_DELETE_OBJECTS; break;
            case CANCEL_PENDING_OWNERSHIP_ACQUISITIONS: wire = Common.ResignAction.RESIGN_ACTION_CANCEL_PENDING_OWNERSHIP; break;
            case DELETE_OBJECTS_THEN_DIVEST: wire = Common.ResignAction.RESIGN_ACTION_DELETE_THEN_DIVEST; break;
            case CANCEL_THEN_DELETE_THEN_DIVEST: wire = Common.ResignAction.RESIGN_ACTION_CANCEL_THEN_DELETE; break;
            case NO_ACTION: wire = Common.ResignAction.RESIGN_ACTION_NO_ACTION; break;
            default: throw new InvalidResignAction("Unsupported resign action");
        }
        rpc(() -> FederationServiceGrpc.newBlockingStub(session.channel()).resignFederation(
                Federation.ResignFederationRequest.newBuilder().setWireVersion(V1).setFederationName(federation)
                        .setFederateHandle(handle).setAction(wire).build()));
        epoch++;
        try {
            session.clearMembership();
        } catch (RuntimeException failure) {
            throw new RTIinternalError("Resigned, but callback teardown failed", failure);
        } finally {
            callbacks.clear();
            transportationAdmissionRetries.clear();
            streamFailure = null;
            handle = 0;
            federation = null;
            resetTime();
        }
    }

    public void enableTimeRegulation(LogicalTimeInterval lookahead) throws RTIexception {
        member();
        if (regulationPending) throw new RequestForTimeRegulationPending("Enabled callback is pending");
        if (advancePending) throw new InTimeAdvancingState("Grant callback is pending");
        if (regulating) throw new TimeRegulationAlreadyEnabled("Time regulation is enabled");
        LogicalTimeInterval value = lookahead(lookahead);
        rpc(() -> timeStub().enableTimeRegulation(TimeWire.writeInterval(Time.EnableRegulationRequest.newBuilder().setWireVersion(V1)
                .setFederationName(federation).setFederateHandle(handle),"lookahead",value,timeImplementation()).build()));
        regulationPending = true;
    }
    public void disableTimeRegulation() throws RTIexception {
        member();
        if (!regulating) throw new TimeRegulationIsNotEnabled("The enabled callback has not been invoked");
        rpc(() -> timeStub().disableTimeRegulation(Time.DisableRegulationRequest.newBuilder().setWireVersion(V1)
                .setFederationName(federation).setFederateHandle(handle).build()));
        regulating = false;
    }
    public void enableTimeConstrained() throws RTIexception {
        member();
        if (constrainedPending) throw new RequestForTimeConstrainedPending("Enabled callback is pending");
        if (advancePending) throw new InTimeAdvancingState("Grant callback is pending");
        if (constrained) throw new TimeConstrainedAlreadyEnabled("Time constrained is enabled");
        rpc(() -> timeStub().enableTimeConstrained(Time.EnableConstrainedRequest.newBuilder().setWireVersion(V1)
                .setFederationName(federation).setFederateHandle(handle).build()));
        constrainedPending = true;
    }
    public void disableTimeConstrained() throws RTIexception {
        member();
        if (!constrained) throw new TimeConstrainedIsNotEnabled("The enabled callback has not been invoked");
        rpc(() -> timeStub().disableTimeConstrained(Time.DisableConstrainedRequest.newBuilder().setWireVersion(V1)
                .setFederationName(federation).setFederateHandle(handle).build()));
        constrained = false;
        exchange.constrainDisabled();
    }
    public void timeAdvanceRequest(LogicalTime time) throws RTIexception {
        advanceReady(); LogicalTime value = time(time);
        rpc(() -> timeStub().timeAdvanceRequest(TimeWire.write(Time.TARRequest.newBuilder().setWireVersion(V1)
                .setFederationName(federation).setFederateHandle(handle),"logical_time",value,timeImplementation()).build()));
        advancePending = true;
    }
    public void timeAdvanceRequestAvailable(LogicalTime time) throws RTIexception {
        advanceReady(); LogicalTime value = time(time);
        rpc(() -> timeStub().timeAdvanceRequestAvailable(TimeWire.write(Time.TARARequest.newBuilder().setWireVersion(V1)
                .setFederationName(federation).setFederateHandle(handle),"logical_time",value,timeImplementation()).build()));
        advancePending = true;
    }
    public void nextMessageRequest(LogicalTime time) throws RTIexception {
        advanceReady(); LogicalTime value = time(time);
        rpc(() -> timeStub().nextMessageRequest(TimeWire.write(Time.NERRequest.newBuilder().setWireVersion(V1)
                .setFederationName(federation).setFederateHandle(handle),"logical_time",value,timeImplementation()).build()));
        advancePending = true;
    }
    public void nextMessageRequestAvailable(LogicalTime time) throws RTIexception {
        advanceReady(); LogicalTime value = time(time);
        rpc(() -> timeStub().nextMessageRequestAvailable(TimeWire.write(Time.NMRARequest.newBuilder().setWireVersion(V1)
                .setFederationName(federation).setFederateHandle(handle),"logical_time",value,timeImplementation()).build()));
        advancePending = true;
    }
    public void flushQueueRequest(LogicalTime time) throws RTIexception {
        advanceReady(); LogicalTime value = time(time);
        rpc(() -> timeStub().flushQueueRequest(TimeWire.write(Time.FQRRequest.newBuilder().setWireVersion(V1)
                .setFederationName(federation).setFederateHandle(handle),"logical_time",value,timeImplementation()).build()));
        advancePending = true;
    }
    public void enableAsynchronousDelivery() throws RTIexception {
        member();
        rpc(() -> timeStub().enableAsynchronousDelivery(Time.EnableAsynchronousDeliveryRequest.newBuilder()
                .setWireVersion(V1).setFederationName(federation).setFederateHandle(handle).build()));
    }
    public void disableAsynchronousDelivery() throws RTIexception {
        member();
        rpc(() -> timeStub().disableAsynchronousDelivery(Time.DisableAsynchronousDeliveryRequest.newBuilder()
                .setWireVersion(V1).setFederationName(federation).setFederateHandle(handle).build()));
    }
    public void modifyLookahead(LogicalTimeInterval interval) throws RTIexception {
        member(); LogicalTimeInterval value = lookahead(interval);
        if (advancePending) throw new InTimeAdvancingState("Grant callback is pending");
        if (!regulating) throw new TimeRegulationIsNotEnabled("Time regulation is not enabled");
        rpc(() -> timeStub().modifyLookahead(TimeWire.writeInterval(Time.ModifyLookaheadRequest.newBuilder().setWireVersion(V1)
                .setFederationName(federation).setFederateHandle(handle),"lookahead",value,timeImplementation()).build()));
    }
    public LogicalTime queryLogicalTime() throws RTIexception {
        member();
        LogicalTime serverTime = TimeWire.read(rpc(() -> timeStub().queryLogicalTime(query())),"logical_time",timeImplementation());
        return advancePending || regulationPending || constrainedPending ? logicalTime : serverTime;
    }
    public LogicalTimeInterval queryLookahead() throws RTIexception {
        member();
        if (!regulating) throw new TimeRegulationIsNotEnabled("Time regulation is not enabled");
        return TimeWire.readInterval(rpc(() -> timeStub().queryLookahead(query())),"lookahead",timeImplementation());
    }
    public TimeQueryReturn queryGALT() throws RTIexception {
        member(); Time.QueryGALTResponse result = rpc(() -> timeStub().queryGALT(query()));
        return new TimeQueryReturn(result.getFinite(), result.getFinite() ? TimeWire.read(result,"galt",timeImplementation()) : null);
    }
    public TimeQueryReturn queryLITS() throws RTIexception {
        member(); Time.QueryLITSResponse result = rpc(() -> timeStub().queryLITS(query()));
        return new TimeQueryReturn(result.getFinite(), result.getFinite() ? TimeWire.read(result,"lits",timeImplementation()) : null);
    }
    public LogicalTimeFactory getTimeFactory() throws RTIexception {
        joined();
        if (joinedTimeFactory == null) throw new FederateNotExecutionMember("No accepted logical time factory; join cleanup is pending");
        return joinedTimeFactory;
    }
    public AttributeHandleFactory getAttributeHandleFactory() throws RTIexception { joined(); return Handles.attributeFactory(); }
    public DimensionHandleFactory getDimensionHandleFactory() throws RTIexception { joined(); return Handles.dimensionFactory(); }
    public FederateHandleFactory getFederateHandleFactory() throws RTIexception { joined(); return Handles.federateFactory(); }
    public InteractionClassHandleFactory getInteractionClassHandleFactory() throws RTIexception { joined(); return Handles.interactionClassFactory(); }
    public ObjectClassHandleFactory getObjectClassHandleFactory() throws RTIexception { joined(); return Handles.objectClassFactory(); }
    public ObjectInstanceHandleFactory getObjectInstanceHandleFactory() throws RTIexception { joined(); return Handles.objectInstanceFactory(); }
    public ParameterHandleFactory getParameterHandleFactory() throws RTIexception { joined(); return Handles.parameterFactory(); }
    public TransportationTypeHandleFactory getTransportationTypeHandleFactory() throws RTIexception { joined(); return Handles.transportationFactory(); }
    public AttributeHandleSetFactory getAttributeHandleSetFactory() throws RTIexception { joined(); return HandleCollections.attributeSetFactory(); }
    public FederateHandleSetFactory getFederateHandleSetFactory() throws RTIexception { joined(); return HandleCollections.federateSetFactory(); }
    public DimensionHandleSetFactory getDimensionHandleSetFactory() throws RTIexception { joined(); return HandleCollections.dimensionSetFactory(); }
    public RegionHandleSetFactory getRegionHandleSetFactory() throws RTIexception { joined(); return HandleCollections.regionSetFactory(); }
    public AttributeHandleValueMapFactory getAttributeHandleValueMapFactory() throws RTIexception { joined(); return HandleCollections.attributeValueMapFactory(); }
    public ParameterHandleValueMapFactory getParameterHandleValueMapFactory() throws RTIexception { joined(); return HandleCollections.parameterValueMapFactory(); }
    public AttributeSetRegionSetPairListFactory getAttributeSetRegionSetPairListFactory() throws RTIexception { joined(); return HandleCollections.attributeRegionListFactory(); }
    public String getHLAversion() throws RTIexception { return "IEEE 1516-2010"; }

    public void enableCallbacks() throws RTIexception {
        callbackSwitchReady();
        callbacksEnabled = true;
    }
    public void enableOwnershipCallbackReceipts() throws RTIexception { member(); ownershipDelivery.enable(); }
    public void retryOwnershipCallbacks() throws RTIexception { member(); ownershipDelivery.retry(); }
    public void disableCallbacks() throws RTIexception {
        callbackSwitchReady();
        callbacksEnabled = false;
    }
    private void callbackSwitchReady() throws RTIexception {
        // Defensive product policy: the interface does not declare NotConnected here.
        if (session == null) throw new RTIinternalError("Connect before changing callback delivery");
        if (callbackSaveLabel != null) throw new SaveInProgress("Federate save is in progress");
        if (callbackRestoreInProgress) throw new RestoreInProgress("Federate restore is in progress");
    }
    private void observeCallbackPhase(Stream.FederateEvent event) {
        if (event.hasSaveInitiate()) admittedSaveLabel = event.getSaveInitiate().getLabel();
        String terminal = event.hasSaveCompleted() ? event.getSaveCompleted().getLabel()
                : event.hasSaveFailed() ? event.getSaveFailed().getLabel() : null;
        if (terminal != null && terminal.equals(callbackSaveLabel)) callbackSaveLabel = null;
        if (terminal != null && terminal.equals(admittedSaveLabel)) admittedSaveLabel = null;
        if (event.hasRestoreCompleted() || event.hasRestoreFailed()) callbackRestoreInProgress = false;
    }
    private boolean deliverSaveRestoreCallback(Stream.FederateEvent event) throws RTIexception {
        if (!CallbackInvocationDelivery.isRecovered(event)) {
            if (event.hasSaveCompleted() || event.hasSaveFailed() || event.hasRestoreCompleted() || event.hasRestoreFailed())
                observeCallbackPhase(event);
            // Start switch guards at user entry, not while initiation is still queued.
            if (event.hasSaveInitiate()) callbackSaveLabel = event.getSaveInitiate().getLabel();
            if (event.hasRestoreBegun() || event.hasRestoreInitiate()) callbackRestoreInProgress = true;
        }
        return SaveRestoreCallbacks.deliver(event, ambassador, timeImplementation());
    }
    public boolean evokeCallback(double minimum) throws RTIexception { return evoke(minimum, minimum, false); }
    public boolean evokeMultipleCallbacks(double minimum, double maximum) throws RTIexception {
        return evoke(minimum, maximum, true);
    }

    private final CallbackDispatcher callbackDispatcher = new CallbackDispatcher();

    private boolean evoke(double minimum, double maximum, boolean multiple) throws RTIexception {
        outsideCallback();
        if (!Double.isFinite(minimum) || !Double.isFinite(maximum) || minimum < 0 || maximum < minimum
                || maximum > 86400) throw new RTIinternalError("Invalid callback wait interval");
        if (streamFailure != null) return connectionLost();
        if (!callbacksEnabled) return !callbacks.isEmpty() || !restoredControlCallbacks.isEmpty() || !restoredLocalNotifications.isEmpty()
                || callbackDispatcher.hasPending(epoch, callbackEpoch);
        callbackDispatcher.beginEvocation();
        RTIexception retryFailure = null;
        try { callbackInvocation.flush(); }
        catch (RTIexception failure) { retryFailure = failure; }
        long start = System.nanoTime();
        long minNanos = (long)(minimum * 1_000_000_000L);
        long maxNanos = (long)(maximum * 1_000_000_000L);
        do {
            Callback next;
            try {
                Stream.FederateEvent restoredControl = restoredControlsReady ? restoredControlCallbacks.poll() : null;
                LocalNotification restoredLocal = restoredControlsReady && restoredControl == null ? restoredLocalNotifications.poll() : null;
                CallbackDispatcher.Pending deferred = restoredControl == null && restoredLocal == null
                        ? callbackDispatcher.poll(epoch, callbackEpoch) : null;
                if (restoredLocal != null) next = () -> dispatchLocal(restoredLocal);
                else if (restoredControl != null) next = () -> deliver(restoredControl, restoreGate.get());
                else if (deferred != null) next = () -> {
                    if (epoch == deferred.joinedEpoch && callbackEpoch == deferred.callbackEpoch)
                        deliver(deferred.event, deferred.restoreGate);
                };
                else next = callbacks.poll(Math.max(0, minNanos - (System.nanoTime() - start)), TimeUnit.NANOSECONDS);
            } catch (InterruptedException failure) {
                Thread.currentThread().interrupt();
                throw new RTIinternalError("Callback wait interrupted", failure);
            }
            if (streamFailure != null) return connectionLost();
            if (next == null) break;
            callbackDispatcher.beginDispatch();
            inCallback = true;
            try { next.invoke(); }
            catch (RTIexception failure) {
                if (retryFailure != null && retryFailure != failure) failure.addSuppressed(retryFailure);
                throw new RTIinternalError("Federate callback failed", failure);
            }
            finally { inCallback = false; }
        } while (callbacksEnabled && (callbackDispatcher.deferred() || multiple && System.nanoTime() - start < maxNanos));
        if (retryFailure != null) throw retryFailure;
        return !callbacks.isEmpty() || callbackDispatcher.hasPending(epoch, callbackEpoch)
                || restoredControlsReady && (!restoredControlCallbacks.isEmpty() || !restoredLocalNotifications.isEmpty());
    }

    private void deliver(Stream.FederateEvent event, long admittedRestoreGate) throws RTIexception {
        long joinedEpoch = epoch;
        long restoredEpoch = callbackEpoch;
        if (callbackInvocation.recover(event)) return;
        if (callbackDispatcher.retained(event, joinedEpoch, restoredEpoch)) return;
        if (!ownershipDelivery.begin(event)) { ownershipDelivery.flush(); return; }
        if (event.hasLocalValidationBarrier() || event.hasHalted()) {
            if (event.hasHalted()) streamFailure = new IllegalStateException(event.getHalted().getCause());
            return;
        }
        final rti.v1.Object.TransportationChange[] activatedTransportation = {null};
        try {
            if (!callbackInvocation.begin(event, () -> {
                if (event.hasTransportationChangeConfirmed()) {
                    activatedTransportation[0] = transportation.activateStreamConfirmation(event.getTransportationChangeConfirmed());
                }
            })) return;
        } catch (RTIexception | RuntimeException | Error failure) {
            ownershipDelivery.entryFailed(event);
            if ((failure instanceof SaveInProgress || failure instanceof RestoreInProgress)
                    && (event.hasSyncRegistrationSucceeded() || event.hasSyncRegistrationFailed())) {
                callbackDispatcher.defer(event, admittedRestoreGate, joinedEpoch, restoredEpoch);
                return;
            }
            if (event.hasTransportationChangeConfirmed() && failure instanceof Exception)
                retainTransportationAdmission(event, admittedRestoreGate);
            throw failure;
        }
        Throwable failed = null;
        try {
            switch (event.getEventCase()) {
                case TRANSPORTATION_CHANGE_CONFIRMED:
                    transportation.confirmStream(activatedTransportation[0], ambassador); break;
                case TIME_REGULATION_ENABLED:
                    logicalTime = TimeWire.read(event.getTimeRegulationEnabled(),"logical_time",timeImplementation());
                    regulationPending = false;
                    regulating = true;
                    ambassador.timeRegulationEnabled(logicalTime); break;
                case TIME_CONSTRAINED_ENABLED:
                    logicalTime = TimeWire.read(event.getTimeConstrainedEnabled(),"logical_time",timeImplementation());
                    constrainedPending = false;
                    constrained = true;
                    ambassador.timeConstrainedEnabled(logicalTime); break;
                case GRANT:
                    logicalTime = TimeWire.read(event.getGrant(),"logical_time",timeImplementation());
                    advancePending = false;
                    exchange.advanceKnowledge(logicalTime);
                    ambassador.timeAdvanceGrant(logicalTime); break;
                case HALTED:
                    streamFailure = new IllegalStateException(event.getHalted().getCause()); break;
                case LOCAL_VALIDATION_BARRIER: return;
                default:
                    if (event.hasSaveInitiate() && TimeWire.hasTime(event.getSaveInitiate(), "save_time")) {
                        TimeWire.read(event.getSaveInitiate(), "save_time", timeImplementation());
                    }
                    if (!CallbackInvocationDelivery.isRecovered(event)) beforeStatefulCallback(event, admittedRestoreGate);
                    beforeExchangeCallback(event);
                    if (!OwnershipCallbacks.deliver(event, ambassador)
                            && !deliverSaveRestoreCallback(event)
                            && !ExchangeCallbacks.deliver(event, ambassador, federation, federationGeneration, () -> {
                        Stream.RemoveObjectInstance removed = event.getRemove();
                        Stream.ObjectCallbackMetadata metadata = removed.getMetadata();
                        exchange.removed(removed.getObjectHandle(),
                                metadata.getReceivedOrder() == rti.v1.Object.OrderType.ORDER_TYPE_TIMESTAMP,
                                metadata.getProducingFederate(), metadata.getMessageRetractionHandle(),
                                TimeWire.hasTime(removed, "logical_time") ? TimeWire.read(removed, "logical_time", timeImplementation()) : null);
                    }, timeImplementation())) {
                        throw new RTIinternalError("Java binding does not yet implement callback " + event.getEventCase());
                    }
            }
        } catch (RTIexception | RuntimeException | Error failure) {
            failed = failure;
            throw failure;
        } finally {
            if (!CallbackInvocationDelivery.isRecovered(event) && event.hasRestoreCompleted()
                    && hydratedCallbackEpoch == event.getRestoreCompleted().getCallbackEpoch())
                restoredControlsReady = true;
            RTIexception pendingReportFailure = null;
            try { ownershipDelivery.flush(); }
            catch (RTIexception reportFailure) {
                if (failed != null) failed.addSuppressed(reportFailure); else pendingReportFailure = reportFailure;
            }
            try { callbackInvocation.complete(event, failed); }
            catch (RTIexception reportFailure) {
                if (failed != null) failed.addSuppressed(reportFailure);
                else if (pendingReportFailure != null) pendingReportFailure.addSuppressed(reportFailure);
                else pendingReportFailure = reportFailure;
            }
            if (pendingReportFailure != null) throw pendingReportFailure;
        }
    }

    private void retainTransportationAdmission(Stream.FederateEvent event, long admittedRestoreGate) {
        ByteString receipt = event.getCallbackReceipt();
        if (!transportationAdmissionRetries.add(receipt)) return;
        long joinedEpoch = epoch;
        long restoredEpoch = callbackEpoch;
        try {
            enqueue(new Callback() {
                @Override public void invoke() throws RTIexception {
                    transportationAdmissionRetries.remove(receipt);
                    if (epoch == joinedEpoch && callbackEpoch == restoredEpoch)
                        deliver(event, admittedRestoreGate);
                }
                @Override public void retire() { transportationAdmissionRetries.remove(receipt); }
            });
        } catch (RuntimeException failure) {
            transportationAdmissionRetries.remove(receipt);
            throw failure;
        }
    }

    private void reportGenericCallback(Stream.FederateEvent event, boolean success, String exception, boolean entry) throws RTIexception {
        boolean historic = CallbackInvocationDelivery.isRecovered(event);
        long fallbackHandle = historic ? 0 : event.hasRestoreCompleted() ? event.getRestoreCompleted().getFederateHandle()
                : event.getRestoreInitiate().getFederateHandle();
        if (!historic && event.hasRestoreInitiate() && !event.getRestoreInitiate().getClientState().isEmpty()) {
            try {
                RuntimeCheckpoint target = RuntimeCheckpoint.decodeForRestore(event.getRestoreInitiate().getClientState().toByteArray(),
                        federation, federationGeneration, event.getRestoreInitiate().getLabel());
                target.validateTime(timeImplementation());
                fallbackHandle = target.persistedHandle;
            } catch (IOException invalid) { throw new RTIinternalError("Cannot identify restored callback participant", invalid); }
        }
        final long restoredHandle = fallbackHandle;
        Support.ReportCallbackInvocationRequest report = Support.ReportCallbackInvocationRequest.newBuilder()
                .setWireVersion(V1).setFederationName(federation).setFederateHandle(handle)
                .setCallbackReceipt(event.getCallbackReceipt()).setSuccess(success)
                .setException(exception).setInvocationEntry(entry).build();
        rpc(() -> {
            SupportServiceGrpc.SupportServiceBlockingStub stub = SupportServiceGrpc.newBlockingStub(session.channel());
            try { return stub.reportCallbackInvocation(report); }
            catch (StatusRuntimeException rejected) {
                io.grpc.Status.Code code = rejected.getStatus().getCode();
                if (restoredHandle == 0 || restoredHandle == report.getFederateHandle()
                        || (code != io.grpc.Status.Code.FAILED_PRECONDITION
                        && code != io.grpc.Status.Code.PERMISSION_DENIED)) throw rejected;
                return stub.reportCallbackInvocation(report.toBuilder().setFederateHandle(restoredHandle).build());
            }
        });
    }

    private void enqueue(Callback callback) {
        if (!callbacks.offer(callback)) {
            IllegalStateException failure = new IllegalStateException("Java callback queue capacity exceeded");
            streamFailure = failure;
            throw failure;
        }
    }
    private void enqueueFederateCallback(CallbackSink.Invocation callback, long joinedEpoch, long restoredEpoch) {
        LocalNotification notification = callback instanceof LocalNotification ? (LocalNotification) callback : null;
        if (notification != null && notification.order == 0) {
            if (++nextLocalOrder == 0) throw new IllegalStateException("Local callback order exhausted");
            notification = notification.ordered(nextLocalOrder);
        }
        LocalNotification queued = notification; long connectedEpoch = connectionEpoch;
        enqueue(new Callback() {
            @Override public boolean retired() {
                return queued != null && queued.scope.equals("connection") ? connectedEpoch != connectionEpoch
                        : epoch != joinedEpoch || callbackEpoch != restoredEpoch;
            }
            @Override public void invoke() throws RTIexception {
                if (retired()) return;
                if (queued == null) callback.invoke(ambassador); else dispatchLocal(queued);
            }
            @Override public LocalNotification checkpointNotification() { return retired() ? null : queued; }
            @Override public boolean connectionScoped() { return queued != null && queued.scope.equals("connection"); }
        });
    }

    private void dispatchLocal(LocalNotification notification) throws RTIexception {
        // A restored connection-scoped result can also survive in the source queue.
        // Purge queued copies before entry, including when the user body throws.
        restoredLocalNotifications.removeIf(queued -> queued.localId.equals(notification.localId));
        callbacks.removeIf(callback -> {
            LocalNotification queued = callback.checkpointNotification();
            return queued != null && queued.localId.equals(notification.localId);
        });
        notification.invoke(ambassador);
    }

    private void beforeStatefulCallback(Stream.FederateEvent event, long admittedRestoreGate) throws RTIexception {
        if (event.hasRestoreBegun() || event.hasRestoreFailed()) {
            pendingRestoreCheckpoint = null;
            pendingRestoreLabel = null;
            pendingRestoreHandle = 0;
            pendingRestoreFailure = null;
            pendingRestoreValidated = false;
        }
        if (event.hasRestoreFailed()) completeRestoreAttempt(admittedRestoreGate);
        if (event.hasRestoreInitiate()) {
            pendingRestoreValidated = false;
            pendingRestoreFailure = null;
            Stream.InitiateFederateRestore initiated = event.getRestoreInitiate();
            pendingRestoreLabel = initiated.getLabel();
            pendingRestoreHandle = initiated.getFederateHandle();
            try {
                pendingRestoreCheckpoint = initiated.getClientState().isEmpty() ? checkpoints.get(pendingRestoreLabel)
                        : RuntimeCheckpoint.decodeForRestore(initiated.getClientState().toByteArray(), federation, federationGeneration, pendingRestoreLabel);
                if (pendingRestoreCheckpoint == null) throw new IOException("No persisted Java SDK state in this legacy checkpoint");
                pendingRestoreCheckpoint.validateTime(timeImplementation());
                if (pendingRestoreCheckpoint.persistedHandle != 0) pendingRestoreHandle = pendingRestoreCheckpoint.persistedHandle;
                pendingRestoreValidated = true;
            } catch (IOException failure) {
                pendingRestoreFailure = new RTIinternalError("Cannot restore Java SDK runtime state", failure);
                throw pendingRestoreFailure;
            }
        }
        if (event.hasSaveInitiate()) {
            pendingSaveLabel = event.getSaveInitiate().getLabel();
            pendingCheckpoint = new RuntimeCheckpoint(exchange.checkpoint(), ddm.checkpoint(), synchronization.checkpoint(),
                    regulating, constrained, regulationPending, constrainedPending, advancePending, logicalTime, pendingStreamCallbacks());
        }
        saveTerminal(event);
        if (!event.hasRestoreCompleted()) return;
        Stream.FederationRestored restored = event.getRestoreCompleted();
        hydrateRestoreState(restored);
        pendingRestoreValidated = false;
        appliedCallbackEpoch = restored.getCallbackEpoch();
        completeRestoreAttempt(admittedRestoreGate);
    }

    private void hydrateRestoreState(Stream.FederationRestored restored) throws RTIinternalError {
        if (hydratedCallbackEpoch == restored.getCallbackEpoch()) return;
        RestoreHydration hydration;
        synchronized (restoreHandoff) { hydration = restoreHydration; }
        if (hydration == null || hydration.joinedEpoch != epoch || hydration.restored != restored) {
            RTIinternalError failure = new RTIinternalError("Restored state has no matching transport identity");
            streamFailure = failure;
            throw failure;
        }
        // Retiring old terminals retains committed save bookkeeping, not old user callbacks.
        hydration.retire();
        if (restored.getLabel().equals(pendingRestoreLabel) && pendingRestoreHandle != restored.getFederateHandle()) {
            RTIinternalError failure = new RTIinternalError("Restored membership differs from the announced checkpoint identity");
            streamFailure = failure;
            throw failure;
        }
        RuntimeCheckpoint saved = restored.getLabel().equals(pendingRestoreLabel) ? pendingRestoreCheckpoint : checkpoints.get(restored.getLabel());
        if (saved == null) {
            RTIinternalError failure = new RTIinternalError("No Java local checkpoint for restore label: "
                    + restored.getLabel());
            streamFailure = failure;
            throw failure;
        }
        try { saved.validateTime(timeImplementation()); }
        catch (IOException invalid) { throw new RTIinternalError("Invalid Java runtime checkpoint", invalid); }
        try {
            session.rebindMembership(restored.getFederateHandle());
            handle = restored.getFederateHandle();
            exchange.restore(saved.exchangeState, restored.getNextRetractionHandle());
            synchronization.restore(saved.synchronizations);
            ddm.restore(saved.ddmState);
            transportation.clear();
            transportationAdmissionRetries.clear();
            restoredControlCallbacks.clear();
            restoredLocalNotifications.clear();
            restoredLocalNotifications.addAll(saved.pendingLocalNotifications);
            for (LocalNotification notification : saved.pendingLocalNotifications)
                if (Long.compareUnsigned(notification.order, nextLocalOrder) > 0) nextLocalOrder = notification.order;
            restoredControlsReady = false;
            callbackInvocation.restoreOutcomes(saved.pendingOutcomes);
            for (Stream.FederateEvent event : saved.pendingCallbacks) {
                // Receipt-bearing events are replayed with fresh tickets by the server.
                if (!saved.serverRecovery && event.getCallbackReceipt().isEmpty() && !event.hasOwnershipReceipt())
                    restoredControlCallbacks.add(event);
            }
            regulating = saved.regulating;
            constrained = saved.constrained;
            regulationPending = saved.regulationPending;
            constrainedPending = saved.constrainedPending;
            advancePending = saved.advancePending;
        try { logicalTime = TimeWire.checked(saved.logicalTimeValue, timeImplementation()); }
        catch (IllegalArgumentException invalid) { throw new RTIinternalError("Checkpoint logical-time factory mismatch", invalid); }
            hydratedCallbackEpoch = restored.getCallbackEpoch();
        } catch (RuntimeException failure) {
            streamFailure = failure;
            throw new RTIinternalError("Failed to apply Java local restore state", failure);
        }
    }

    private void completeRestoreAttempt(long admittedGate) {
        // Odd tokens identify pending attempts; an older terminal must not clear a newer one.
        if ((admittedGate & 1L) != 0) restoreGate.compareAndSet(admittedGate, admittedGate + 1L);
    }

    private void saveTerminal(Stream.FederateEvent event) {
        String label = event.hasSaveCompleted() ? event.getSaveCompleted().getLabel()
                : event.hasSaveFailed() ? event.getSaveFailed().getLabel() : null;
        if (label == null || !label.equals(pendingSaveLabel)) return;
        if (event.hasSaveCompleted() && pendingCheckpoint != null) checkpoints.put(label, pendingCheckpoint);
        pendingSaveLabel = null;
        pendingCheckpoint = null;
    }

    private byte[] checkpointBytes() throws RTIinternalError {
        if (inCallback) throw new RTIinternalError("Cannot checkpoint a running callback body");
        List<Stream.FederateEvent> pendingEvents = pendingStreamCallbacks();
        if (pendingCheckpoint == null) return new byte[0];
        try {
            pendingCheckpoint = new RuntimeCheckpoint(exchange.checkpoint(), ddm.checkpoint(), synchronization.checkpoint(),
                    regulating, constrained, regulationPending, constrainedPending, advancePending, logicalTime, pendingEvents)
                    .withRecovery(callbackInvocation.checkpointOutcomes()).withLocalNotifications(pendingLocalNotifications());
            return RuntimeJson.encode(pendingCheckpoint, federation, federationGeneration, handle, pendingSaveLabel);
        } catch (IOException failure) { throw new RTIinternalError("Cannot encode Java SDK checkpoint", failure); }
    }

    private List<Stream.FederateEvent> pendingStreamCallbacks() throws RTIinternalError {
        List<Stream.FederateEvent> events = new ArrayList<>();
        events.addAll(callbackDispatcher.checkpointEvents(epoch, callbackEpoch));
        events.addAll(restoredControlCallbacks);
        for (Callback callback : callbacks) {
            Stream.FederateEvent event = callback.checkpointEvent();
            if (event == null && callback.checkpointNotification() == null && !callback.retired())
                throw new RTIinternalError("Uncheckpointable queued local callback");
            if (event != null && RuntimeCheckpoint.journalEvent(event)) events.add(event);
        }
        return events;
    }

    private List<LocalNotification> pendingLocalNotifications() {
        List<LocalNotification> result = new ArrayList<>(); Set<String> seen = new HashSet<>();
        for (LocalNotification notification : restoredLocalNotifications)
            if (seen.add(notification.localId)) result.add(notification);
        for (Callback callback : callbacks) {
            LocalNotification notification = callback.checkpointNotification();
            if (notification != null && seen.add(notification.localId)) result.add(notification);
        }
        // Order is a FIFO ordinal in this saved local queue, not a server stream sequence.
        for (int i = 0; i < result.size(); i++) result.set(i, result.get(i).ordered(i + 1L));
        return result;
    }

    private void restoreReady() throws RTIexception {
        if (!callbackRestoreInProgress) throw new RestoreNotRequested("Federate restore has not been requested");
        if (pendingRestoreFailure != null) throw pendingRestoreFailure;
        if (!pendingRestoreValidated) throw new RTIinternalError("Java runtime restore state has not been validated");
    }
    private void beforeExchangeCallback(Stream.FederateEvent event) {
        switch (event.getEventCase()) {
            case DISCOVER:
                exchange.discovered(event.getDiscover().getObjectHandle(), event.getDiscover().getObjectClassHandle(),
                        event.getDiscover().getObjectName()); break;
            case RETRACTION_REQUESTED:
                exchange.retracted(event.getRetractionRequested().getSenderFederate(), event.getRetractionRequested().getMessageRetractionHandle()); break;
            case SYNC_ANNOUNCED: synchronization.announced(event.getSyncAnnounced().getLabel()); break;
            case SYNC_SYNCHRONIZED: synchronization.completed(event.getSyncSynchronized().getLabel()); break;
            default: break;
        }
    }
    @Override public void requireMember() throws RTIexception { member(); }
    @Override public void requireRestoreControl() throws RTIexception { memberTransportReady(); }
    @Override public void requireRestoreResponse() throws RTIexception {
        memberTransportReady();
        if (admittedSaveLabel != null || callbackSaveLabel != null)
            throw new SaveInProgress("Federation save is in progress");
    }
    @Override public Channel channel() { return session.channel(); }
    @Override public String federation() { return federation; }
    @Override public long federate() { return handle; }
    @Override public long generation() { return federationGeneration; }
    @Override public boolean regulating() { return regulating; }
    @Override public double logicalTime() { return TimeValues.value(logicalTime); }
    @Override public LogicalTime logicalTimeValue() { return logicalTime; }
    @Override public String timeImplementation() { return joinedTimeFactory==null ? FLOAT64_TIME : joinedTimeFactory.getName(); }
    @Override public long callbackEpoch() { return callbackEpoch; }
    @Override public void flushOwnershipCallbacks() throws RTIexception { ownershipDelivery.flush(); }
    @Override public boolean ownershipCallbackReceiptsEnabled() { return ownershipDelivery.enabled(); }
    @Override public void retireObjectKnowledge(long object, long epoch) { ownershipDelivery.retire(object, epoch); }
    private void connected() throws NotConnected {
        if (session == null) throw new NotConnected("Connect first");
    }
    private void joined() throws RTIexception {
        connected();
        if (handle == 0) throw new FederateNotExecutionMember("Join a federation first");
    }
    private void member() throws RTIexception {
        memberTransportReady();
        requireRestoreCallbackDelivered();
        if ((restoreGate.get() & 1L) != 0) throw new RestoreInProgress("Federation restoration is in progress");
        ownershipDelivery.flush();
    }
    private void requireRestoreCallbackDelivered() throws RestoreInProgress {
        if (appliedCallbackEpoch != callbackEpoch) {
            throw new RestoreInProgress("Invoke federationRestored before issuing mutations against the restored state");
        }
    }
    private void memberTransportReady() throws RTIexception {
        joined();
        if (pendingJoinCleanup != null) throw pendingJoinCleanup;
        if (streamFailure != null) throw new RTIinternalError("Callback connection has failed", streamFailure);
        for (;;) {
            RestoreHydration hydration;
            synchronized (restoreHandoff) {
                if (hydratedCallbackEpoch == callbackEpoch) return;
                hydration = restoreHydration;
            }
            if (hydration == null || hydration.joinedEpoch != epoch) {
                throw new RTIinternalError("Restored membership is not available for support queries");
            }
            hydrateRestoreState(hydration.restored);
        }
    }
    private void advanceReady() throws RTIexception {
        member();
        if (advancePending) throw new InTimeAdvancingState("Grant callback is pending");
        if (regulationPending) throw new RequestForTimeRegulationPending("Enabled callback is pending");
        if (constrainedPending) throw new RequestForTimeConstrainedPending("Enabled callback is pending");
    }
    private void resetTime() {
        callbackDispatcher.clear();
        callbackSaveLabel = null;
        admittedSaveLabel = null;
        callbackRestoreInProgress = false;
        ownershipDelivery.reset();
        callbackInvocation.restoreEpoch();
        restoredControlCallbacks.clear();
        restoredLocalNotifications.clear();
        restoredControlsReady = false;
        exchange.clearKnowledge();
        synchronization.clear();
        ddm.clear();
        transportation.clear();
        checkpoints.clear();
        pendingSaveLabel = null;
        pendingCheckpoint = null;
        pendingRestoreCheckpoint = null;
        pendingRestoreLabel = null;
        pendingRestoreHandle = 0;
        pendingRestoreFailure = null;
        pendingRestoreValidated = false;
        synchronized (restoreHandoff) {
            restoreHydration = null;
            callbackEpoch = 0;
            appliedCallbackEpoch = 0;
            hydratedCallbackEpoch = 0;
        }
        restoreGate.set(0);
        regulationPending = false;
        constrainedPending = false;
        advancePending = false;
        regulating = false;
        constrained = false;
        logicalTime = TimeValues.time(0);
        joinedTimeFactory = null;
        callbackEntrySupported = false;
        pendingJoinCleanup = null;
    }
    private boolean connectionLost() throws RTIexception {
        Throwable failure = streamFailure;
        if (!callbacksEnabled) throw new RTIinternalError("Callback connection failed while callbacks are disabled", failure);
        FederateAmbassador target = ambassador;
        WireSession previous = session;
        epoch++;
        connectionEpoch++;
        session = null;
        ambassador = null;
        handle = 0;
        federation = null;
        resetTime();
        callbacks.clear();
        transportationAdmissionRetries.clear();
        try {
            if (previous != null) previous.close();
        } catch (RuntimeException teardown) {
            failure.addSuppressed(teardown);
        } finally {
            callbacks.clear();
            transportationAdmissionRetries.clear();
            streamFailure = null;
        }
        inCallback = true;
        try { target.connectionLost(failure.toString()); }
        catch (FederateInternalError callbackFailure) {
            throw new RTIinternalError("connectionLost callback failed", callbackFailure);
        } finally { inCallback = false; }
        return false;
    }
    private void outsideCallback() throws CallNotAllowedFromWithinCallback {
        if (inCallback) throw new CallNotAllowedFromWithinCallback("Not permitted inside a federate callback");
    }
    private FederationServiceGrpc.FederationServiceBlockingStub federationStub() {
        return FederationServiceGrpc.newBlockingStub(session.unauthenticatedChannel());
    }
    private TimeServiceGrpc.TimeServiceBlockingStub timeStub() { return TimeServiceGrpc.newBlockingStub(session.channel()); }
    private Federation.ListFederationsResponse list() throws RTIexception {
        return rpc(() -> federationStub().listFederations(Federation.ListFederationsRequest.newBuilder().setWireVersion(V1).build()));
    }
    private long generation(String name) throws RTIexception {
        return federationSummary(name).getFederationGeneration();
    }
    private Federation.FederationSummary federationSummary(String name) throws RTIexception {
        for (Federation.FederationSummary entry : list().getFederationsList()) {
            if (entry.getName().equals(name)) return entry;
        }
        throw new FederationExecutionDoesNotExist(name);
    }
    private static String timeImplementationName(String name) {
        return name.isEmpty() ? FLOAT64_TIME : name;
    }
    private static LogicalTimeFactory selectTimeFactory(String name) throws CouldNotCreateLogicalTimeFactory {
        if (name == null) throw new CouldNotCreateLogicalTimeFactory("Logical time implementation name is null");
        String selected = timeImplementationName(name);
        if (!FLOAT64_TIME.equals(selected) && !TimeWire.INTEGER.equals(selected)) {
            throw new CouldNotCreateLogicalTimeFactory("Unsupported federation logical time implementation: " + selected);
        }
        return TimeWire.factory(selected);
    }
    private Time.QueryFederateTimeRequest query() {
        return Time.QueryFederateTimeRequest.newBuilder().setWireVersion(V1).setFederationName(federation).setFederateHandle(handle).build();
    }
    private LogicalTime time(LogicalTime value) throws InvalidLogicalTime {
        try {
            return TimeWire.checked(value,timeImplementation());
        } catch (RuntimeException failure) { throw new InvalidLogicalTime("Expected " + timeImplementation(), failure); }
    }
    private LogicalTimeInterval lookahead(LogicalTimeInterval value) throws InvalidLookahead {
        try {
            return TimeWire.checked(value,timeImplementation());
        } catch (RuntimeException failure) { throw new InvalidLookahead("Expected interval for " + timeImplementation(), failure); }
    }
    private static <T> T rpc(Supplier<T> action) throws RTIexception {
        try { return action.get(); } catch (StatusRuntimeException failure) { throw WireErrors.translate(failure); }
    }
    @FunctionalInterface private interface Callback {
        void invoke() throws RTIexception;
        default void retire() { }
        default boolean connectionScoped() { return false; }
        default Stream.FederateEvent checkpointEvent() { return null; }
        default LocalNotification checkpointNotification() { return null; }
        default boolean retired() { return false; }
    }
    private static final class RestoreHydration {
        final long joinedEpoch;
        final Stream.FederationRestored restored;
        final List<Callback> obsolete;
        RestoreHydration(long joinedEpoch, Stream.FederationRestored restored, List<Callback> obsolete) {
            this.joinedEpoch = joinedEpoch;
            this.restored = restored;
            this.obsolete = obsolete;
        }
        void retire() {
            for (Callback callback : obsolete) callback.retire();
            obsolete.clear();
        }
    }
    private static final class InformationSet extends HashSet<FederationExecutionInformation>
            implements FederationExecutionInformationSet { private static final long serialVersionUID = 1L; }
}
