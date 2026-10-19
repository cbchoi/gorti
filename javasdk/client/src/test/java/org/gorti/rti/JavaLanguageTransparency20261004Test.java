package org.gorti.rti;

import hla.rti1516e.*;
import hla.rti1516e.exceptions.InteractionClassNotPublished;
import java.nio.charset.StandardCharsets;
import java.nio.file.*;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.ConcurrentLinkedQueue;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;
import org.junit.jupiter.api.condition.EnabledIfSystemProperty;
import org.junit.jupiter.api.io.TempDir;

import static org.junit.jupiter.api.Assertions.*;

@EnabledIfSystemProperty(named = "gorti.endpoint", matches = ".+")
class JavaLanguageTransparency20261004Test {
    @TempDir Path temporary;

    @Test
    @Timeout(60)
    void simultaneousJavaAndPythonParticipantsExchangeOpaqueInteractionBytesThroughPublicApis() throws Exception {
        ConcurrentLinkedQueue<Delivery> received = new ConcurrentLinkedQueue<>();
        AtomicReference<String> unexpected = new AtomicReference<>();
        Thread evokingThread = Thread.currentThread();
        boolean[] evoking = {false};
        NullFederateAmbassador callbacks = new NullFederateAmbassador() {
            @Override public void receiveInteraction(InteractionClassHandle interaction, ParameterHandleValueMap parameters,
                    byte[] tag, OrderType sent, TransportationTypeHandle transport, SupplementalReceiveInfo info) {
                if (!evoking[0] || Thread.currentThread() != evokingThread) unexpected.set("Callback outside EVOKED caller boundary");
                received.add(new Delivery(interaction, parameters, tag, sent, transport,
                        info.hasProducingFederate() ? info.getProducingFederate() : null));
            }
            @Override public void connectionLost(String reason) { unexpected.set("connectionLost: " + reason); }
        };
        Path script = temporary.resolve("language_peer.py");
        try (var input = getClass().getResourceAsStream("/java-language-peer-20261004.py")) {
            assertNotNull(input);
            Files.copy(input, script);
        }
        Path ready = temporary.resolve("ready");
        Path log = Path.of(System.getProperty("gorti.evidence.dir")).resolve("python-language-peer.log");
        Process child = null;
        try (var session = new JavaAnnexFdd20261004Test.Session(callbacks)) {
            session.createAndJoin(Files.writeString(temporary.resolve("language.xml"), JavaAnnexFdd20261004Test.model("")));
            RTIambassador rti = session.rti;
            InteractionClassHandle interaction = rti.getInteractionClassHandle("HLAinteractionRoot.LanguageProbe");
            ParameterHandle parameter = rti.getParameterHandle(interaction, "payload");
            byte[] outgoing = {0, (byte) 0x80, (byte) 0xff, 0x7f};
            byte[] tag = {(byte) 0xfe, 0, 0x21};
            ParameterHandleValueMap parameters = rti.getParameterHandleValueMapFactory().create(1);
            parameters.put(parameter, outgoing);
            assertThrows(InteractionClassNotPublished.class, () -> rti.sendInteraction(interaction, parameters, tag));
            rti.publishInteractionClass(interaction);
            rti.subscribeInteractionClass(interaction);
            child = new ProcessBuilder(System.getProperty("gorti.python"), "-u", script.toString(),
                    System.getProperty("gorti.endpoint"), session.name, ready.toString())
                    .redirectErrorStream(true).redirectOutput(log.toFile()).start();
            long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(20);
            while (!Files.exists(ready) && child.isAlive() && System.nanoTime() < deadline) Thread.sleep(20);
            assertTrue(Files.exists(ready), () -> "Python participant did not become ready: " + readLog(log));
            FederateHandle python = rti.getFederateHandle("python");
            assertNotEquals(rti.getFederateHandle("java"), python);
            rti.sendInteraction(interaction, parameters, tag);
            // The sender may receive its own publication. Record only the Python reply.
            deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(20);
            Delivery reply = null;
            while (System.nanoTime() < deadline) {
                evoking[0] = true;
                try { rti.evokeMultipleCallbacks(0.0, 0.05); }
                finally { evoking[0] = false; }
                Delivery candidate = received.poll();
                if (candidate != null && python.equals(candidate.producer)) { reply = candidate; break; }
                assertTrue(child.isAlive(), () -> "Python peer failed: " + readLog(log));
            }
            assertNotNull(reply, () -> "No actual Python reply callback: " + readLog(log));
            assertNull(unexpected.get());
            assertEquals(interaction, reply.interaction);
            assertEquals(1, reply.parameters.size());
            assertArrayEquals(new byte[]{0x7f, (byte) 0xff, (byte) 0x80, 0}, reply.parameters.get(parameter));
            assertArrayEquals(new byte[0], reply.tag, "Python facade untimed send has no user-tag argument");
            assertEquals(OrderType.RECEIVE, reply.order);
            assertEquals(rti.getTransportationTypeHandle("HLAreliable"), reply.transport);
            assertEquals(python, reply.producer);
            assertEquals("python", rti.getFederateName(python), "Python remains joined through actual Java callback assertions");
            child.getOutputStream().write("FINISH\n".getBytes(StandardCharsets.UTF_8));
            child.getOutputStream().flush();
            assertTrue(child.waitFor(10, TimeUnit.SECONDS), "Python cleanup timed out");
            assertEquals(0, child.exitValue(), readLog(log));
            assertTrue(readLog(log).contains("JAVA_TO_PYTHON_ASSERTIONS_PASSED"), readLog(log));
            System.out.println("Simultaneous public Java/Python RO interaction callbacks checked in both directions, opaque bytes/tags/order/transport/producer and EVOKED boundary; no all-language claim.");
        } finally {
            if (child != null && child.isAlive()) {
                child.destroy();
                if (!child.waitFor(5, TimeUnit.SECONDS)) {
                    child.destroyForcibly();
                    assertTrue(child.waitFor(5, TimeUnit.SECONDS), "Owned Python process did not terminate");
                }
            }
        }
    }

    private static String readLog(Path path) {
        try { return Files.exists(path) ? Files.readString(path) : "No peer output"; }
        catch (Exception failure) { return failure.toString(); }
    }

    private static final class Delivery {
        final InteractionClassHandle interaction;
        final ParameterHandleValueMap parameters;
        final byte[] tag;
        final OrderType order;
        final TransportationTypeHandle transport;
        final FederateHandle producer;
        Delivery(InteractionClassHandle interaction, ParameterHandleValueMap parameters, byte[] tag,
                OrderType order, TransportationTypeHandle transport, FederateHandle producer) {
            this.interaction = interaction;
            this.parameters = parameters;
            this.tag = tag.clone();
            this.order = order;
            this.transport = transport;
            this.producer = producer;
        }
    }
}
