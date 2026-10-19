package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;
import hla.rti1516e.*;
import hla.rti1516e.exceptions.*;
import io.grpc.*;
import java.lang.reflect.Field;
import java.net.*;
import java.nio.file.*;
import java.util.*;
import java.util.concurrent.TimeUnit;
import java.util.function.BooleanSupplier;
import org.gorti.rti.transport.WireSession;
import org.junit.jupiter.api.Timeout;
import org.junit.jupiter.api.condition.EnabledIfSystemProperty;
import org.junit.jupiter.api.io.TempDir;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.CsvSource;
import rti.v1.Stream;
import rti.v1.Support;

/** Actual TCP server restart and replacement JVMs; faults live only in a transport wrapper. */
@EnabledIfSystemProperty(named = "gorti.server.binary", matches = ".+")
class LiveControlOutcomeRestoreTest {
    @TempDir Path output;

    @ParameterizedTest @CsvSource({"HLAfloat64Time,true", "HLAfloat64Time,false", "HLAinteger64Time,true", "HLAinteger64Time,false"})
    @Timeout(180)
    void pendingControlAndKnownOutcomeSurviveServerRestartAndFreshJvm(String scalar, boolean success) throws Exception {
        verifyRestart(scalar, success, false);
    }

    @ParameterizedTest @org.junit.jupiter.params.provider.ValueSource(strings = {TimeWire.FLOAT, TimeWire.INTEGER})
    @Timeout(180)
    void pendingPureLocalResultsSurviveServerRestartAndFreshJvm(String scalar) throws Exception {
        verifyRestart(scalar, true, true);
    }

    private void verifyRestart(String scalar, boolean success, boolean local) throws Exception {
        String federation = "java-control-outcome-" + UUID.randomUUID();
        String scenario = scalar + (local ? "-local" : "-" + success);
        Path directory = output.resolve(scenario); Files.createDirectories(directory);
        int port = freePort(), adminPort = freePort(), metricsPort = freePort();
        String endpoint = "grpc://127.0.0.1:" + port;
        Process server = null;
        Path binary = Path.of(System.getProperty("gorti.server.binary"));
        String binaryHash = hash(binary);
        Files.writeString(directory.resolve("binary.txt"), binary.toAbsolutePath() + "\nSHA256=" + binaryHash + "\n");
        try {
            server = startServer(directory, port, adminPort, metricsPort, "source");
            create(endpoint, federation, scalar);
            long sourceGeneration = run(directory, "save", endpoint, federation, scalar, success, local);
            destroy(endpoint, federation);
            stop(server); server = null;
            server = startServer(directory, port, adminPort, metricsPort, "target");
            create(endpoint, federation, scalar);
            long targetGeneration = run(directory, "restore", endpoint, federation, scalar, success, local);
            assertNotEquals(sourceGeneration, targetGeneration, "Test must cross actual execution generations");
            destroy(endpoint, federation);
        } finally {
            if (server != null) stop(server);
            assertEquals(binaryHash, hash(binary), "Assessed binary changed during execution");
            String keep = System.getProperty("gorti.live.evidence");
            if (keep != null) {
                Path destination = Path.of(keep).resolve(scenario); Files.createDirectories(destination);
                try (java.util.stream.Stream<Path> files = Files.list(directory)) {
                    files.filter(Files::isRegularFile).forEach(file -> {
                        try { Files.copy(file, destination.resolve(file.getFileName()), StandardCopyOption.REPLACE_EXISTING); }
                        catch (java.io.IOException failure) { throw new java.io.UncheckedIOException(failure); }
                    });
                }
            }
        }
    }

    private static String hash(Path binary) throws Exception {
        byte[] digest = java.security.MessageDigest.getInstance("SHA-256").digest(Files.readAllBytes(binary));
        StringBuilder hex = new StringBuilder(64);
        for (byte value : digest) hex.append(String.format(Locale.ROOT, "%02x", value & 255));
        return hex.toString();
    }

    private static int freePort() throws Exception { try (ServerSocket socket = new ServerSocket(0)) { return socket.getLocalPort(); } }

    private Process startServer(Path directory, int port, int admin, int metrics, String stage) throws Exception {
        Process process = new ProcessBuilder(System.getProperty("gorti.server.binary"), "-listen", "127.0.0.1:" + port,
                "-admin-listen", "127.0.0.1:" + admin, "-metrics-listen", "127.0.0.1:" + metrics,
                "-save-dir", directory.resolve("saves").toString(), "-state-dir", directory.resolve("state").toString(),
                "-log-level", "warn").redirectErrorStream(true).redirectOutput(directory.resolve(stage + "-server.log").toFile()).start();
        try {
            long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(20);
            while (System.nanoTime() < deadline) {
                if (!process.isAlive()) throw new AssertionError("Server exited: " + Files.readString(directory.resolve(stage + "-server.log")));
                try (Socket socket = new Socket()) { socket.connect(new InetSocketAddress("127.0.0.1", port), 200); return process; }
                catch (java.io.IOException waiting) { Thread.sleep(50); }
            }
            throw new AssertionError("Server did not listen");
        } catch (Throwable failure) { stop(process); throw failure; }
    }

    private static void stop(Process process) throws Exception {
        process.destroy(); if (!process.waitFor(5, TimeUnit.SECONDS)) process.destroyForcibly();
        assertTrue(process.waitFor(10, TimeUnit.SECONDS), "Owned server did not close");
    }

    private void create(String endpoint, String federation, String scalar) throws Exception {
        RTIambassador rti = new GortiRtiAmbassador(); rti.connect(new NullFederateAmbassador(), CallbackModel.HLA_EVOKED, endpoint);
        try { rti.createFederationExecution(federation, new URL[]{getClass().getResource("/exchange-fom.xml")}, scalar); }
        finally { rti.disconnect(); }
    }
    private void destroy(String endpoint, String federation) throws Exception {
        RTIambassador rti = new GortiRtiAmbassador(); rti.connect(new NullFederateAmbassador(), CallbackModel.HLA_EVOKED, endpoint);
        try { rti.destroyFederationExecution(federation); } finally { rti.disconnect(); }
    }

    private long run(Path directory, String mode, String endpoint, String federation, String scalar, boolean success, boolean local) throws Exception {
        Path evidence = directory.resolve(mode + "-child.log");
        Process child = new ProcessBuilder(Path.of(System.getProperty("java.home"), "bin",
                System.getProperty("os.name").startsWith("Windows") ? "java.exe" : "java").toString(),
                "-cp", System.getProperty("surefire.test.class.path", System.getProperty("java.class.path")),
                (local ? LocalParticipant.class : Participant.class).getName(), mode, endpoint, federation, scalar, Boolean.toString(success))
                .redirectErrorStream(true).redirectOutput(evidence.toFile()).start();
        try {
            assertTrue(child.waitFor(55, TimeUnit.SECONDS), "Child timed out");
            String log = Files.readString(evidence); assertEquals(0, child.exitValue(), log);
            assertTrue(log.contains((local ? "LOCAL_NOTIFICATIONS_" : "CONTROL_OUTCOME_") + mode.toUpperCase() + "_OK"), log);
            java.util.regex.Matcher generation = java.util.regex.Pattern.compile("generation=([0-9]+)").matcher(log);
            assertTrue(generation.find(), log); return Long.parseUnsignedLong(generation.group(1));
        } finally {
            if (child.isAlive()) { child.destroyForcibly(); assertTrue(child.waitFor(5, TimeUnit.SECONDS)); }
        }
    }

    public static final class LocalParticipant {
        public static void main(String[] args) throws Exception {
            boolean save = args[0].equals("save");
            System.out.println("LOCAL_NOTIFICATIONS_PHASE start mode=" + args[0] + " scalar=" + args[3]);
            GortiRtiAmbassador rti = new GortiRtiAmbassador();
            LocalEvents events = new LocalEvents(save, args[2], args[3]); boolean joined = false;
            rti.connect(events, CallbackModel.HLA_EVOKED, args[1]);
            Field field = GortiRtiAmbassador.class.getDeclaredField("services"); field.setAccessible(true);
            AmbassadorServices services = (AmbassadorServices) field.get(rti);
            events.currentHandle = services::federate;
            Field sessionField = AmbassadorServices.class.getDeclaredField("session"); sessionField.setAccessible(true);
            WireSession session = (WireSession) sessionField.get(services);
            Field transportField = WireSession.class.getDeclaredField("transport"); transportField.setAccessible(true);
            ManagedChannel underlying = (ManagedChannel) transportField.get(session);
            int[] queries = {0};
            transportField.set(session, new QueryCountingTransport(underlying, queries));
            try {
                events.handle = rti.joinFederationExecution("local-participant", "java-local-model", args[2]); joined = true;
                queries[0] = 0;
                if (save) {
                    rti.requestFederationSave("local"); Participant.await(rti, () -> events.saving, false);
                    rti.queryFederationSaveStatus(); rti.queryFederationSaveStatus(); rti.listFederationExecutions();
                    byte[] bytes = LocalNotificationCheckpointTest.export(services);
                    Field generation = AmbassadorServices.class.getDeclaredField("federationGeneration"); generation.setAccessible(true);
                    RuntimeCheckpoint captured = RuntimeCheckpoint.decodeForRestore(bytes, args[2], generation.getLong(services), "local");
                    if (captured.pendingLocalNotifications.size() != 3 || !events.trace.isEmpty() || queries[0] != 3)
                        throw new AssertionError("Local source setup did not preserve three uninvoked RPC results");
                    rti.federateSaveBegun(); rti.federateSaveComplete(); Participant.await(rti, () -> events.saved, false);
                } else {
                    rti.requestFederationRestore("local"); Participant.await(rti, () -> events.restoring, false);
                    rti.federateRestoreComplete(); Participant.await(rti, () -> events.restored && events.trace.size() == 3, false);
                    for (int i = 0; i < 5; i++) rti.evokeMultipleCallbacks(0, 0.02);
                    if (queries[0] != 0) throw new AssertionError("Restored pure results repeated an RPC");
                }
                if (!events.trace.equals(List.of("save-status", "save-status", "executions")))
                    throw new AssertionError("Local FIFO/multiplicity changed: " + events.trace);
                Field generation = AmbassadorServices.class.getDeclaredField("federationGeneration"); generation.setAccessible(true);
                System.out.println("LOCAL_NOTIFICATIONS_" + args[0].toUpperCase() + "_OK scalar=" + args[3]
                        + " generation=" + Long.toUnsignedString(generation.getLong(services)) + " queries=" + queries[0]);
            } finally {
                if (joined) rti.resignFederationExecution(ResignAction.NO_ACTION);
                rti.disconnect();
            }
        }
    }

    private static final class LocalEvents extends NullFederateAmbassador {
        final boolean source; final String federation, scalar;
        final List<String> trace = new ArrayList<>();
        FederateHandle handle;
        java.util.function.LongSupplier currentHandle;
        boolean saving, saved, restoring, restored;
        LocalEvents(boolean source, String federation, String scalar) { this.source = source; this.federation = federation; this.scalar = scalar; }
        private void entered() { if (!source && !restored) throw new AssertionError("Local result preceded restore terminal"); }
        @Override public void federationSaveStatusResponse(FederateHandleSaveStatusPair[] statuses) {
            entered(); trace.add("save-status");
            if (statuses.length != 1 || !statuses[0].handle.equals(org.gorti.rti.types.Handles.federate(currentHandle.getAsLong()))
                    || statuses[0].status != SaveStatus.FEDERATE_INSTRUCTED_TO_SAVE)
                throw new AssertionError("Historical save result or rebound member changed");
        }
        @Override public void reportFederationExecutions(FederationExecutionInformationSet executions) {
            entered(); trace.add("executions");
            if (executions.stream().noneMatch(value -> value.federationExecutionName.equals(federation)
                    && value.logicalTimeImplementationName.equals(scalar))) throw new AssertionError("Historical execution result changed");
        }
        @Override public void initiateFederateSave(String label) { saving = true; }
        @Override public void federationSaved() { saved = true; }
        @Override public void initiateFederateRestore(String label, String name, FederateHandle handle) { this.handle = handle; restoring = true; }
        @Override public void federationRestored() { restored = true; }
    }

    private static final class QueryCountingTransport extends ManagedChannel {
        final ManagedChannel delegate; final int[] queries;
        QueryCountingTransport(ManagedChannel delegate, int[] queries) { this.delegate = delegate; this.queries = queries; }
        @Override public <ReqT, RespT> ClientCall<ReqT, RespT> newCall(MethodDescriptor<ReqT, RespT> method, CallOptions options) {
            if (method.getBareMethodName().equals("QuerySaveState") || method.getBareMethodName().equals("ListFederations")) queries[0]++;
            return delegate.newCall(method, options);
        }
        @Override public ManagedChannel shutdown() { delegate.shutdown(); return this; }
        @Override public boolean isShutdown() { return delegate.isShutdown(); }
        @Override public boolean isTerminated() { return delegate.isTerminated(); }
        @Override public ManagedChannel shutdownNow() { delegate.shutdownNow(); return this; }
        @Override public boolean awaitTermination(long timeout, TimeUnit unit) throws InterruptedException { return delegate.awaitTermination(timeout, unit); }
        @Override public String authority() { return delegate.authority(); }
    }

    public static final class Participant {
        public static void main(String[] args) throws Exception {
            boolean save = args[0].equals("save"), success = Boolean.parseBoolean(args[4]);
            System.out.println("CONTROL_OUTCOME_PHASE start mode=" + args[0] + " success=" + success + " scalar=" + args[3]);
            GortiRtiAmbassador rti = new GortiRtiAmbassador(); Events events = new Events(success); boolean joined = false;
            rti.connect(events, CallbackModel.HLA_EVOKED, args[1]);
            Field field = GortiRtiAmbassador.class.getDeclaredField("services"); field.setAccessible(true);
            AmbassadorServices services = (AmbassadorServices) field.get(rti);
            Field sessionField = AmbassadorServices.class.getDeclaredField("session"); sessionField.setAccessible(true);
            WireSession session = (WireSession) sessionField.get(services);
            Field transportField = WireSession.class.getDeclaredField("transport"); transportField.setAccessible(true);
            FaultTransport transport = new FaultTransport((ManagedChannel) transportField.get(session), save, success);
            transportField.set(session, transport);
            Field deliveryField = AmbassadorServices.class.getDeclaredField("callbackInvocation"); deliveryField.setAccessible(true);
            CallbackInvocationDelivery delivery = (CallbackInvocationDelivery) deliveryField.get(services);
            try {
                rti.joinFederationExecution("outcome-participant", "java-outcome-model", args[2]); joined = true;
                System.out.println("CONTROL_OUTCOME_PHASE joined");
                if (save) {
                    rti.registerFederationSynchronizationPoint("finished", new byte[]{1});
                    await(rti, () -> events.finished == 1, true);
                    System.out.println("CONTROL_OUTCOME_PHASE finished-result-retained");
                    rti.registerFederationSynchronizationPoint("pending", new byte[]{2});
                    await(rti, () -> transport.held == 1, true);
                    System.out.println("CONTROL_OUTCOME_PHASE pending-control-held");
                    if (delivery.checkpointOutcomes().size() != 1 || events.pending != 0) throw new AssertionError("Fault setup did not retain pending work");
                    CallbackInvocationDelivery.Outcome outcome = delivery.checkpointOutcomes().get(0);
                    if (outcome.success != success || !outcome.exception.equals(success ? "" : "FederateInternalError"))
                        throw new AssertionError("Saved outcome differs from finished application result");
                    rti.requestFederationSave("outcome"); await(rti, () -> events.saving, true);
                    System.out.println("CONTROL_OUTCOME_PHASE save-initiated");
                    rti.federateSaveBegun(); rti.federateSaveComplete(); await(rti, () -> events.saved, true);
                } else {
                    rti.requestFederationRestore("outcome"); await(rti, () -> events.restoring, false);
                    System.out.println("CONTROL_OUTCOME_PHASE restore-initiated");
                    rti.federateRestoreComplete(); await(rti, () -> events.restored && transport.recoveredReports == 1 && events.pending == 1, false);
                    for (int i = 0; i < 5; i++) rti.evokeMultipleCallbacks(0, 0.02);
                    if (events.finished != 0 || events.pending != 1 || transport.recoveredReports != 1 || !delivery.checkpointOutcomes().isEmpty())
                        throw new AssertionError("Recovered body repeated or pending outcome retained");
                }
                Field generation = AmbassadorServices.class.getDeclaredField("federationGeneration"); generation.setAccessible(true);
                System.out.println("CONTROL_OUTCOME_" + args[0].toUpperCase() + "_OK success=" + success + " scalar=" + args[3]
                        + " generation=" + Long.toUnsignedString(generation.getLong(services)));
            } finally {
                if (joined) rti.resignFederationExecution(ResignAction.NO_ACTION);
                rti.disconnect();
            }
        }

        private static void await(RTIambassador rti, BooleanSupplier ready, boolean expectedFailure) throws Exception {
            long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(20);
            while (!ready.getAsBoolean() && System.nanoTime() < deadline) {
                try { rti.evokeCallback(0.01); }
                catch (RTIinternalError failure) { if (!expectedFailure) throw failure; }
            }
            if (!ready.getAsBoolean()) throw new AssertionError("Expected callback not received");
        }
    }

    private static final class Events extends NullFederateAmbassador {
        final boolean success;
        int finished, pending;
        boolean saving, saved, restoring, restored;
        Events(boolean success) { this.success = success; }
        @Override public void announceSynchronizationPoint(String label, byte[] tag) throws FederateInternalError {
            if (label.equals("pending")) { pending++; if (!restored) throw new AssertionError("Pending control preceded terminal"); }
            else if (label.equals("finished")) { finished++; if (!success) throw new FederateInternalError("Known completed application failure"); }
        }
        @Override public void initiateFederateSave(String label) { saving = true; }
        @Override public void federationSaved() { saved = true; }
        @Override public void initiateFederateRestore(String label, String name, FederateHandle handle) { restoring = true; }
        @Override public void federationRestored() { restored = true; }
    }

    private static final class FaultTransport extends ManagedChannel {
        final ManagedChannel delegate;
        final boolean save, success;
        final Map<com.google.protobuf.ByteString, Stream.FederateEvent> carriers = new java.util.concurrent.ConcurrentHashMap<>();
        volatile int held, recoveredReports;
        FaultTransport(ManagedChannel delegate, boolean save, boolean success) { this.delegate = delegate; this.save = save; this.success = success; }

        @Override public <ReqT, RespT> ClientCall<ReqT, RespT> newCall(MethodDescriptor<ReqT, RespT> method, CallOptions options) {
            ClientCall<ReqT, RespT> call = delegate.newCall(method, options);
            if (method.getFullMethodName().equals("rti.v1.StreamService/EventBatches")) {
                return new ForwardingClientCall.SimpleForwardingClientCall<ReqT, RespT>(call) {
                    @Override public void start(Listener<RespT> listener, Metadata headers) {
                        super.start(new ForwardingClientCallListener.SimpleForwardingClientCallListener<RespT>(listener) {
                            @Override @SuppressWarnings("unchecked") public void onMessage(RespT message) {
                                Stream.FederateEventBatch batch = (Stream.FederateEventBatch) message;
                                Stream.FederateEventBatch.Builder filtered = batch.toBuilder().clearEvents();
                                for (Stream.FederateEvent event : batch.getEventsList()) {
                                    if (!event.getCallbackReceipt().isEmpty()) carriers.put(event.getCallbackReceipt(), event);
                                    if (save && event.hasSyncAnnounced() && event.getSyncAnnounced().getLabel().equals("pending")) { held++; continue; }
                                    filtered.addEvents(event);
                                }
                                super.onMessage((RespT) filtered.build());
                            }
                        }, headers);
                    }
                };
            }
            if (!method.getFullMethodName().equals("rti.v1.SupportService/ReportCallbackInvocation")) return call;
            return new ClientCall<ReqT, RespT>() {
                Listener<RespT> listener; Metadata headers; int demand; boolean started, blocked;
                @Override public void start(Listener<RespT> listener, Metadata headers) { this.listener = listener; this.headers = headers; }
                @Override public void request(int count) { if (started) call.request(count); else demand += count; }
                @Override public void sendMessage(ReqT message) {
                    Support.ReportCallbackInvocationRequest report = (Support.ReportCallbackInvocationRequest) message;
                    Stream.FederateEvent event = carriers.get(report.getCallbackReceipt());
                    boolean finished = event != null && event.hasSyncAnnounced() && event.getSyncAnnounced().getLabel().equals("finished");
                    if (save && finished && !report.getInvocationEntry()) {
                        blocked = true; listener.onClose(Status.UNAVAILABLE.withDescription("Injected completed outcome report outage"), new Metadata()); return;
                    }
                    if (!save && finished) {
                        if (report.getInvocationEntry() || report.getSuccess() != success || !report.getException().equals(success ? "" : "FederateInternalError")
                                || !CallbackInvocationDelivery.isOutcomeOnly(event)) throw new AssertionError("Recovery must report known outcome without Entry");
                        recoveredReports++;
                    }
                    call.start(listener, headers); started = true; if (demand > 0) call.request(demand); call.sendMessage(message);
                }
                @Override public void halfClose() { if (started) call.halfClose(); }
                @Override public void cancel(String message, Throwable cause) { if (started) call.cancel(message, cause); else if (!blocked && listener != null) listener.onClose(Status.CANCELLED, new Metadata()); }
            };
        }
        @Override public String authority() { return delegate.authority(); }
        @Override public ManagedChannel shutdown() { delegate.shutdown(); return this; }
        @Override public ManagedChannel shutdownNow() { delegate.shutdownNow(); return this; }
        @Override public boolean isShutdown() { return delegate.isShutdown(); }
        @Override public boolean isTerminated() { return delegate.isTerminated(); }
        @Override public boolean awaitTermination(long timeout, TimeUnit unit) throws InterruptedException { return delegate.awaitTermination(timeout, unit); }
    }
}
