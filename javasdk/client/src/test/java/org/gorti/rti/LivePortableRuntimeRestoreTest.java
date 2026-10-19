package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;
import hla.rti1516e.*;
import hla.rti1516e.exceptions.TimeRegulationAlreadyEnabled;
import hla.rti1516e.time.HLAinteger64Time;
import java.net.URL;
import java.nio.file.*;
import java.util.UUID;
import java.util.concurrent.TimeUnit;
import java.util.function.BooleanSupplier;
import org.gorti.rti.types.Handles;
import org.gorti.rti.types.Integer64TimeFactory;
import org.gorti.rti.types.TimeValues;
import org.junit.jupiter.api.Timeout;
import org.junit.jupiter.api.condition.EnabledIfSystemProperty;
import org.junit.jupiter.api.io.TempDir;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.ValueSource;

/** Independent replacement JVMs, including an actually recreated execution. */
@EnabledIfSystemProperty(named = "gorti.endpoint", matches = ".+")
class LivePortableRuntimeRestoreTest {
    @TempDir Path output;

    @ParameterizedTest @ValueSource(strings = {TimeWire.FLOAT, TimeWire.INTEGER}) @Timeout(120)
    void recreatedExecutionRestoresKnowledgeExactTimeAndRetractionAllocationInFreshJvm(String scalar) throws Exception {
        String federation = "java-portable-runtime-" + UUID.randomUUID();
        String endpoint = System.getProperty("gorti.endpoint");
        RTIambassador admin = new GortiRtiAmbassador();
        admin.connect(new NullFederateAmbassador(), CallbackModel.HLA_EVOKED, endpoint);
        boolean created = false;
        try {
            admin.createFederationExecution(federation, new URL[]{getClass().getResource("/exchange-fom.xml")}, scalar); created = true;
            run("save", endpoint, federation, scalar);
            admin.destroyFederationExecution(federation); created = false;
            admin.createFederationExecution(federation, new URL[]{getClass().getResource("/exchange-fom.xml")}, scalar); created = true;
            run("restore", endpoint, federation, scalar);
        } finally {
            try { if (created) admin.destroyFederationExecution(federation); }
            finally { admin.disconnect(); }
        }
    }

    private void run(String mode, String endpoint, String federation, String scalar) throws Exception {
        Path evidence = output.resolve(mode + "-" + scalar + ".log");
        String executable = Path.of(System.getProperty("java.home"), "bin",
                System.getProperty("os.name").startsWith("Windows") ? "java.exe" : "java").toString();
        String classpath = System.getProperty("surefire.test.class.path", System.getProperty("java.class.path"));
        Process process = new ProcessBuilder(executable, "-cp", classpath, Participant.class.getName(), mode, endpoint, federation, scalar)
                .redirectErrorStream(true).redirectOutput(evidence.toFile()).start();
        try {
            assertTrue(process.waitFor(45, TimeUnit.SECONDS), "Child process timed out: " + mode);
            String log = Files.readString(evidence); assertEquals(0, process.exitValue(), log);
            assertTrue(log.contains("PORTABLE_" + mode.toUpperCase() + "_OK"), log);
            String keep = System.getProperty("gorti.live.evidence");
            if (keep != null) { Path directory = Path.of(keep); Files.createDirectories(directory); Files.copy(evidence, directory.resolve(evidence.getFileName()), StandardCopyOption.REPLACE_EXISTING); }
        } finally {
            if (process.isAlive()) { process.destroyForcibly(); assertTrue(process.waitFor(5, TimeUnit.SECONDS)); }
        }
    }

    public static final class Participant {
        public static void main(String[] args) throws Exception {
            boolean save = args[0].equals("save"), integer = args[3].equals(TimeWire.INTEGER), joined = false;
            RTIambassador rti = new GortiRtiAmbassador(); Events events = new Events();
            rti.connect(events, CallbackModel.HLA_EVOKED, args[1]);
            LogicalTime time = integer ? new Integer64TimeFactory().makeTime((1L << 53) + 1) : TimeValues.time(2.5);
            LogicalTime future = integer ? new Integer64TimeFactory().makeTime((1L << 53) + 9) : TimeValues.time(10);
            LogicalTimeInterval lookahead = integer ? new Integer64TimeFactory().makeInterval(1) : TimeValues.interval(0.5);
            try {
                rti.joinFederationExecution("portable-participant", "java-portable-model", args[2]); joined = true;
                if (save) {
                    ObjectClassHandle type = rti.getObjectClassHandle("HLAobjectRoot.Item");
                    AttributeHandleSet attributes = rti.getAttributeHandleSetFactory().create(); attributes.add(rti.getAttributeHandle(type, "value"));
                    rti.publishObjectClassAttributes(type, attributes);
                    rti.reserveObjectInstanceName("portable-item"); await(rti, () -> events.reserved);
                    ObjectInstanceHandle object = rti.registerObjectInstance(type, "portable-item");
                    rti.enableTimeRegulation(lookahead); await(rti, () -> events.regulating);
                    rti.timeAdvanceRequest(time); await(rti, () -> events.granted);
                    AttributeHandleValueMap values = rti.getAttributeHandleValueMapFactory().create(1); values.put(rti.getAttributeHandle(type, "value"), new byte[]{1});
                    MessageRetractionReturn issued = rti.updateAttributeValues(object, values, new byte[]{2}, future);
                    if (!issued.retractionHandleIsValid || Handles.value(issued.handle) != 1) throw new AssertionError("Expected first saved retraction sequence");
                    rti.requestFederationSave("portable"); await(rti, () -> events.saving);
                    rti.federateSaveBegun(); rti.federateSaveComplete(); await(rti, () -> events.saved);
                } else {
                    rti.requestFederationRestore("portable"); await(rti, () -> events.restoring);
                    rti.federateRestoreComplete(); await(rti, () -> events.restored);
                    ObjectInstanceHandle object = rti.getObjectInstanceHandle("portable-item");
                    ObjectClassHandle type = rti.getKnownObjectClassHandle(object);
                    if (!type.equals(rti.getObjectClassHandle("HLAobjectRoot.Item"))) throw new AssertionError("Lost object knowledge");
                    LogicalTime actual = rti.queryLogicalTime();
                    if (integer ? ((HLAinteger64Time)actual).getValue() != ((HLAinteger64Time)time).getValue() : TimeValues.value(actual) != 2.5)
                        throw new AssertionError("Lost exact logical time");
                    try { rti.enableTimeRegulation(lookahead); throw new AssertionError("Lost regulation state"); }
                    catch (TimeRegulationAlreadyEnabled expected) { }
                    AttributeHandleValueMap values = rti.getAttributeHandleValueMapFactory().create(1); values.put(rti.getAttributeHandle(type, "value"), new byte[]{3});
                    MessageRetractionReturn issued = rti.updateAttributeValues(object, values, new byte[]{4}, future);
                    if (!issued.retractionHandleIsValid || Long.compareUnsigned(Handles.value(issued.handle), 1) <= 0)
                        throw new AssertionError("Restored allocator reused saved retraction sequence");
                    FederateHandle installed = rti.getFederateHandle("portable-participant");
                    if (Handles.retractionProducer(issued.handle) != Handles.value(installed)) throw new AssertionError("Retraction used source producer");
                }
                System.out.println("PORTABLE_" + args[0].toUpperCase() + "_OK");
            } finally {
                if (joined) rti.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST);
                rti.disconnect();
            }
        }
        private static void await(RTIambassador rti, BooleanSupplier ready) throws Exception {
            long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(15);
            while (!ready.getAsBoolean() && System.nanoTime() < deadline) rti.evokeMultipleCallbacks(0.001, 0.02);
            if (!ready.getAsBoolean()) throw new AssertionError("Portable checkpoint callback not delivered");
        }
    }
    private static final class Events extends NullFederateAmbassador {
        boolean reserved, regulating, granted, saving, saved, restoring, restored;
        @Override public void objectInstanceNameReservationSucceeded(String name) { reserved = true; }
        @Override public void timeRegulationEnabled(LogicalTime time) { regulating = true; }
        @Override public void timeAdvanceGrant(LogicalTime time) { granted = true; }
        @Override public void initiateFederateSave(String label) { saving = true; }
        @Override public void federationSaved() { saved = true; }
        @Override public void initiateFederateRestore(String label, String name, FederateHandle handle) { restoring = true; }
        @Override public void federationRestored() { restored = true; }
    }
}
