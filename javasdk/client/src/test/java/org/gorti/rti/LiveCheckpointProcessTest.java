package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;
import hla.rti1516e.*;
import hla.rti1516e.exceptions.TimeRegulationAlreadyEnabled;
import java.net.URL;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.UUID;
import java.util.concurrent.TimeUnit;
import java.util.function.BooleanSupplier;
import org.gorti.rti.types.TimeValues;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;
import org.junit.jupiter.api.condition.EnabledIfSystemProperty;
import org.junit.jupiter.api.io.TempDir;

/** The second JVM has no access to the first JVM's provider state or heap. */
@EnabledIfSystemProperty(named = "gorti.endpoint", matches = ".+")
class LiveCheckpointProcessTest {
    @TempDir Path output;

    @Test @Timeout(100)
    void restoresSdkStateInASeparateReplacementJvm() throws Exception {
        String federation = "java-runtime-process-" + UUID.randomUUID();
        String endpoint = System.getProperty("gorti.endpoint");
        try {
            run("save", endpoint, federation);
            run("restore", endpoint, federation);
        } finally {
            RTIambassador admin = RtiFactoryFactory.getRtiFactory("gorti").getRtiAmbassador();
            admin.connect(new NullFederateAmbassador(), CallbackModel.HLA_EVOKED, endpoint);
            try { admin.destroyFederationExecution(federation); }
            finally { admin.disconnect(); }
        }
    }

    private void run(String mode, String endpoint, String federation) throws Exception {
        String executable = Path.of(System.getProperty("java.home"), "bin", System.getProperty("os.name").startsWith("Windows") ? "java.exe" : "java").toString();
        String classpath = System.getProperty("surefire.test.class.path", System.getProperty("java.class.path"));
        Path log = output.resolve(mode + ".log");
        Process child = new ProcessBuilder(executable, "-cp", classpath, Participant.class.getName(), mode, endpoint, federation)
                .redirectErrorStream(true).redirectOutput(log.toFile()).start();
        try {
            assertTrue(child.waitFor(40, TimeUnit.SECONDS), "Replacement JVM timed out: " + mode);
            String evidence = Files.readString(log);
            assertEquals(0, child.exitValue(), evidence);
            assertTrue(evidence.contains("CHECKPOINT_" + mode.toUpperCase() + "_OK"), evidence);
        } finally {
            if (child.isAlive()) { child.destroyForcibly(); assertTrue(child.waitFor(5, TimeUnit.SECONDS)); }
        }
    }

    public static final class Participant {
        public static void main(String[] args) throws Exception {
            boolean save = args[0].equals("save"), joined = false;
            RTIambassador rti = RtiFactoryFactory.getRtiFactory("gorti").getRtiAmbassador();
            Events events = new Events();
            rti.connect(events, CallbackModel.HLA_EVOKED, args[1]);
            try {
                if (save) rti.createFederationExecution(args[2], new URL[]{Participant.class.getResource("/exchange-fom.xml")});
                rti.joinFederationExecution("checkpoint-participant", "java-process-checkpoint", args[2]); joined = true;
                if (save) {
                    ObjectClassHandle type = rti.getObjectClassHandle("HLAobjectRoot.Item");
                    AttributeHandleSet attributes = rti.getAttributeHandleSetFactory().create();
                    attributes.add(rti.getAttributeHandle(type, "value"));
                    rti.publishObjectClassAttributes(type, attributes);
                    rti.reserveObjectInstanceName("checkpoint-item");
                    await(rti, () -> events.reserved);
                    rti.registerObjectInstance(type, "checkpoint-item");
                    rti.enableTimeRegulation(TimeValues.interval(0.5)); await(rti, () -> events.regulating);
                    rti.timeAdvanceRequest(TimeValues.time(2)); await(rti, () -> events.granted);
                    rti.requestFederationSave("runtime"); await(rti, () -> events.saveInitiated);
                    rti.federateSaveBegun(); rti.federateSaveComplete(); await(rti, () -> events.saved);
                } else {
                    rti.requestFederationRestore("runtime"); await(rti, () -> events.restoreInitiated);
                    rti.federateRestoreComplete(); await(rti, () -> events.restored);
                    ObjectInstanceHandle object = rti.getObjectInstanceHandle("checkpoint-item");
                    if (!rti.getObjectClassHandle("HLAobjectRoot.Item").equals(rti.getKnownObjectClassHandle(object))) throw new AssertionError("Lost local object knowledge");
                    if (TimeValues.value(rti.queryLogicalTime()) != 2) throw new AssertionError("Lost restored logical time");
                    try { rti.enableTimeRegulation(TimeValues.interval(0.5)); throw new AssertionError("Lost restored regulation state"); }
                    catch (TimeRegulationAlreadyEnabled expected) { }
                    AttributeHandleValueMap values = rti.getAttributeHandleValueMapFactory().create(1);
                    values.put(rti.getAttributeHandle(rti.getKnownObjectClassHandle(object), "value"), new byte[]{1});
                    rti.updateAttributeValues(object, values, new byte[0]);
                }
                System.out.println("CHECKPOINT_" + args[0].toUpperCase() + "_OK");
            } finally {
                if (joined) rti.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST);
                rti.disconnect();
            }
        }

        private static void await(RTIambassador rti, BooleanSupplier complete) throws Exception {
            long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(12);
            while (!complete.getAsBoolean() && System.nanoTime() < deadline) rti.evokeMultipleCallbacks(0.001, 0.02);
            if (!complete.getAsBoolean()) throw new AssertionError("Expected checkpoint callback was not delivered");
        }
    }

    private static final class Events extends NullFederateAmbassador {
        boolean reserved, regulating, granted, saveInitiated, saved, restoreInitiated, restored;
        @Override public void objectInstanceNameReservationSucceeded(String name) { reserved = true; }
        @Override public void timeRegulationEnabled(LogicalTime time) { regulating = true; }
        @Override public void timeAdvanceGrant(LogicalTime time) { granted = true; }
        @Override public void initiateFederateSave(String label) { saveInitiated = true; }
        @Override public void federationSaved() { saved = true; }
        @Override public void initiateFederateRestore(String label, String name, FederateHandle handle) { restoreInitiated = true; }
        @Override public void federationRestored() { restored = true; }
    }
}
