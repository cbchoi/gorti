package org.gorti.examples;

import hla.rti1516e.AttributeHandle;
import hla.rti1516e.AttributeHandleSet;
import hla.rti1516e.AttributeHandleValueMap;
import hla.rti1516e.CallbackModel;
import hla.rti1516e.FederateHandle;
import hla.rti1516e.FederateHandleSet;
import hla.rti1516e.InteractionClassHandle;
import hla.rti1516e.NullFederateAmbassador;
import hla.rti1516e.ObjectClassHandle;
import hla.rti1516e.ObjectInstanceHandle;
import hla.rti1516e.OrderType;
import hla.rti1516e.ParameterHandle;
import hla.rti1516e.ParameterHandleValueMap;
import hla.rti1516e.RTIambassador;
import hla.rti1516e.ResignAction;
import hla.rti1516e.RtiFactoryFactory;
import hla.rti1516e.SynchronizationPointFailureReason;
import hla.rti1516e.TransportationTypeHandle;
import hla.rti1516e.exceptions.FederatesCurrentlyJoined;
import hla.rti1516e.exceptions.FederationExecutionDoesNotExist;
import hla.rti1516e.exceptions.NameNotFound;
import java.net.URI;
import java.net.URL;
import java.nio.ByteBuffer;
import java.nio.ByteOrder;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Duration;
import java.util.Arrays;
import java.util.HashSet;
import java.util.Set;
import java.util.concurrent.TimeoutException;

/** Run one publisher or receiver per JVM, using only the public IEEE 1516e API. */
public final class ExchangeFederate {
    private static final String OBJECT_NAME = "java-exchange-item";
    private static final String READY = "ready";
    private static final String DONE = "done";
    private static final byte[] VALUE = integer32(0x0080ff7f);
    private static final byte[] PAYLOAD = integer32(0xff007f80);
    private static final byte[] UPDATE_TAG = {0, (byte) 0xff, 0x11};
    private static final byte[] PULSE_TAG = {(byte) 0x80, 0, 0x22};

    private final String role;
    private final String endpoint;
    private final String federation;
    private final Path fom;
    private final Duration timeout;
    private final RTIambassador rti;
    private final Callbacks callbacks = new Callbacks();
    private boolean connected;
    private boolean created;
    private boolean joined;

    private ExchangeFederate() throws Exception {
        role = System.getProperty("gorti.role", "");
        require("publisher".equals(role) || "receiver".equals(role),
                "Set gorti.role to publisher or receiver");
        endpoint = System.getProperty("gorti.endpoint", "");
        URI address = URI.create(endpoint);
        require(("grpc".equals(address.getScheme()) || "grpcs".equals(address.getScheme()))
                        && address.getHost() != null && address.getPort() > 0 && address.getPort() <= 65535
                        && address.getRawUserInfo() == null && address.getRawQuery() == null
                        && address.getRawFragment() == null
                        && (address.getRawPath().isEmpty() || "/".equals(address.getRawPath())),
                "Set gorti.endpoint to grpc://host:port or grpcs://host:port");
        federation = System.getProperty("gorti.federation", "java-exchange");
        require(!federation.isBlank(), "gorti.federation must not be blank");
        String configuredFom = System.getProperty("gorti.fom", "");
        require(!configuredFom.isBlank(), "Set gorti.fom to examples/java-exchange/exchange-fom.xml");
        fom = Path.of(configuredFom).toAbsolutePath().normalize();
        require(Files.isRegularFile(fom), "FOM file not found: " + fom);
        int seconds = Integer.parseInt(System.getProperty("gorti.timeoutSeconds", "30"));
        require(seconds >= 1 && seconds <= 300, "gorti.timeoutSeconds must be between 1 and 300");
        timeout = Duration.ofSeconds(seconds);
        rti = RtiFactoryFactory.getRtiFactory("gorti").getRtiAmbassador();
    }

    public static void main(String[] args) throws Throwable {
        new ExchangeFederate().run();
    }

    private void run() throws Throwable {
        Throwable failure = null;
        try {
            rti.connect(callbacks, CallbackModel.HLA_EVOKED, endpoint);
            connected = true;
            log("connected " + endpoint);
            Budget budget = new Budget(timeout);
            if (publisher()) {
                rti.createFederationExecution(federation, new URL[] {fom.toUri().toURL()});
                created = true;
                log("created " + federation);
            }
            log("joining " + federation);
            await(budget, "join " + federation, () -> {
                try {
                    rti.joinFederationExecution(role, "JavaExchange", federation);
                    joined = true;
                    return true;
                } catch (FederationExecutionDoesNotExist notCreated) {
                    if (publisher()) throw notCreated;
                    return false;
                }
            });
            log("joined as " + role);
            setup();
            String peer = publisher() ? "receiver" : "publisher";
            log("waiting for " + peer);
            await(budget, "federate " + peer, () -> {
                try {
                    FederateHandle handle = rti.getFederateHandle(peer);
                    if (!publisher()) callbacks.producer = handle;
                    return true;
                } catch (NameNotFound notJoined) {
                    return false;
                }
            });

            // Join visibility is not subscription readiness; the ready barrier covers both.
            synchronize(READY, publisher(), budget);
            if (publisher()) {
                publish(budget);
            } else {
                await(budget, "Item.value and Pulse.payload", () -> callbacks.reflected && callbacks.received);
                log("verified bytes, tags, receive order and publisher metadata");
            }
            synchronize(DONE, !publisher(), budget);
        } catch (Throwable error) {
            failure = error;
        } finally {
            if (joined) {
                failure = cleanup(failure, "resigned", () -> {
                    rti.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST);
                    joined = false;
                });
            }
            if (created) {
                failure = cleanup(failure, "destroyed " + federation, this::destroy);
            }
            if (connected) {
                failure = cleanup(failure, "disconnected", rti::disconnect);
            }
        }
        if (failure != null) throw failure;
        log("exchange complete");
    }

    private boolean publisher() {
        return "publisher".equals(role);
    }

    private void setup() throws Exception {
        callbacks.model = new Model(rti);
        Model model = callbacks.model;
        AttributeHandleSet attributes = rti.getAttributeHandleSetFactory().create();
        attributes.add(model.value);
        if (publisher()) {
            rti.publishObjectClassAttributes(model.item, attributes);
            rti.publishInteractionClass(model.pulse);
            log("published Item.value and Pulse");
        } else {
            rti.subscribeObjectClassAttributes(model.item, attributes);
            rti.subscribeInteractionClass(model.pulse);
            log("subscribed Item.value and Pulse");
        }
    }

    private void synchronize(String label, boolean register, Budget budget) throws Exception {
        if (register) {
            rti.registerFederationSynchronizationPoint(label, syncTag(label));
            await(budget, label + " registration", () -> callbacks.registered.contains(label));
        }
        await(budget, label + " announcement", () -> callbacks.announced.contains(label));
        rti.synchronizationPointAchieved(label);
        await(budget, label + " federationSynchronized", () -> callbacks.synchronizedPoints.contains(label));
        log("synchronized " + label);
    }

    private void publish(Budget budget) throws Exception {
        Model model = callbacks.model;
        rti.reserveObjectInstanceName(OBJECT_NAME);
        await(budget, "object name reservation", () -> callbacks.reserved);
        ObjectInstanceHandle object = rti.registerObjectInstance(model.item, OBJECT_NAME);
        AttributeHandleValueMap attributes = rti.getAttributeHandleValueMapFactory().create(1);
        attributes.put(model.value, VALUE.clone());
        ParameterHandleValueMap parameters = rti.getParameterHandleValueMapFactory().create(1);
        parameters.put(model.payload, PAYLOAD.clone());
        // Untimed overloads deliberately send receive-order data, even with TimeStamp in the FOM.
        rti.updateAttributeValues(object, attributes, UPDATE_TAG.clone());
        rti.sendInteraction(model.pulse, parameters, PULSE_TAG.clone());
        log("sent Item.value=0x0080ff7f and Pulse.payload=0xff007f80 (RO)");
    }

    private void destroy() throws Exception {
        // Cleanup has its own bounded budget so a timed-out protocol can still release the federation.
        await(new Budget(timeout), "receiver resignation before destroy", () -> {
            try {
                rti.destroyFederationExecution(federation);
                return true;
            } catch (FederatesCurrentlyJoined stillJoined) {
                return false;
            } catch (FederationExecutionDoesNotExist alreadyRemoved) {
                return true;
            }
        });
    }

    private void await(Budget budget, String event, Condition completed) throws Exception {
        while (true) {
            budget.waitSeconds(event);
            if (completed.test()) return;
            rti.evokeCallback(budget.waitSeconds(event));
        }
    }

    private Throwable cleanup(Throwable prior, String description, Action action) {
        try {
            action.run();
            log(description);
        } catch (Throwable failure) {
            System.err.println("[" + role + "] cleanup failed (" + description + "): " + failure);
            if (prior == null) return failure;
            if (prior != failure) prior.addSuppressed(failure);
        }
        return prior;
    }

    private void log(String message) {
        System.out.println("[" + role + "] " + message);
    }

    private static void require(boolean condition, String message) {
        if (!condition) throw new IllegalStateException(message);
    }

    private static byte[] integer32(int value) {
        return ByteBuffer.allocate(Integer.BYTES).order(ByteOrder.BIG_ENDIAN).putInt(value).array();
    }

    private static byte[] syncTag(String label) {
        return ("java-exchange:" + label).getBytes(StandardCharsets.UTF_8);
    }

    @FunctionalInterface
    private interface Condition {
        boolean test() throws Exception;
    }

    @FunctionalInterface
    private interface Action {
        void run() throws Exception;
    }

    private static final class Budget {
        private final long started = System.nanoTime();
        private final long nanos;

        Budget(Duration timeout) {
            nanos = timeout.toNanos();
        }

        double waitSeconds(String event) throws InterruptedException, TimeoutException {
            if (Thread.currentThread().isInterrupted()) {
                throw new InterruptedException("Interrupted while waiting for " + event);
            }
            long remaining = nanos - (System.nanoTime() - started);
            if (remaining <= 0) throw new TimeoutException("Timed out waiting for " + event);
            return Math.min(0.1, remaining / 1_000_000_000.0);
        }
    }

    private static final class Model {
        final ObjectClassHandle item;
        final AttributeHandle value;
        final InteractionClassHandle pulse;
        final ParameterHandle payload;
        final TransportationTypeHandle reliable;

        Model(RTIambassador rti) throws Exception {
            item = rti.getObjectClassHandle("HLAobjectRoot.Item");
            value = rti.getAttributeHandle(item, "value");
            pulse = rti.getInteractionClassHandle("HLAinteractionRoot.Pulse");
            payload = rti.getParameterHandle(pulse, "payload");
            reliable = rti.getTransportationTypeHandle("HLAreliable");
        }
    }

    private final class Callbacks extends NullFederateAmbassador {
        final Set<String> registered = new HashSet<>();
        final Set<String> announced = new HashSet<>();
        final Set<String> synchronizedPoints = new HashSet<>();
        Model model;
        FederateHandle producer;
        ObjectInstanceHandle discovered;
        boolean reserved;
        boolean reflected;
        boolean received;

        @Override
        public void connectionLost(String description) {
            throw new IllegalStateException("Connection lost: " + description);
        }

        @Override
        public void synchronizationPointRegistrationSucceeded(String label) {
            registered.add(label);
        }

        @Override
        public void synchronizationPointRegistrationFailed(String label, SynchronizationPointFailureReason reason) {
            throw new IllegalStateException("Synchronization registration failed: " + label + " (" + reason + ")");
        }

        @Override
        public void announceSynchronizationPoint(String label, byte[] tag) {
            require(READY.equals(label) || DONE.equals(label), "Unexpected synchronization label: " + label);
            require(Arrays.equals(syncTag(label), tag), "Incorrect synchronization tag: " + label);
            announced.add(label);
        }

        @Override
        public void federationSynchronized(String label, FederateHandleSet failed) {
            require(failed.isEmpty(), "Federates failed synchronization: " + label + " " + failed);
            synchronizedPoints.add(label);
        }

        @Override
        public void objectInstanceNameReservationSucceeded(String name) {
            require(OBJECT_NAME.equals(name), "Unexpected reserved object name: " + name);
            reserved = true;
        }

        @Override
        public void objectInstanceNameReservationFailed(String name) {
            throw new IllegalStateException("Object name reservation failed: " + name);
        }

        @Override
        public void discoverObjectInstance(ObjectInstanceHandle object, ObjectClassHandle objectClass, String name) {
            throw new IllegalStateException("Object discovery omitted producing-federate metadata");
        }

        @Override
        public void discoverObjectInstance(ObjectInstanceHandle object, ObjectClassHandle objectClass,
                String name, FederateHandle producingFederate) {
            require(!publisher() && discovered == null, "Unexpected object discovery");
            require(model.item.equals(objectClass) && OBJECT_NAME.equals(name), "Incorrect discovered Item");
            require(producer != null && producer.equals(producingFederate), "Incorrect discovery producer");
            discovered = object;
            log("discovered " + name + " from publisher");
        }

        @Override
        public void reflectAttributeValues(ObjectInstanceHandle object, AttributeHandleValueMap attributes,
                byte[] tag, OrderType sent, TransportationTypeHandle transport, SupplementalReflectInfo info) {
            require(!publisher() && !reflected && object.equals(discovered), "Unexpected Item reflection");
            require(attributes.size() == 1 && Arrays.equals(VALUE, attributes.get(model.value)),
                    "Incorrect Item.value bytes");
            require(Arrays.equals(UPDATE_TAG, tag), "Incorrect Item update tag");
            require(sent == OrderType.RECEIVE && model.reliable.equals(transport), "Incorrect Item order/transport");
            require(info.hasProducingFederate() && producer.equals(info.getProducingFederate()),
                    "Incorrect Item producing-federate metadata");
            require(!info.hasSentRegions(), "Unexpected Item regions");
            reflected = true;
            log("verified Item.value (RO)");
        }

        @Override
        public void receiveInteraction(InteractionClassHandle interaction, ParameterHandleValueMap parameters,
                byte[] tag, OrderType sent, TransportationTypeHandle transport, SupplementalReceiveInfo info) {
            require(!publisher() && !received && model.pulse.equals(interaction), "Unexpected Pulse interaction");
            require(parameters.size() == 1 && Arrays.equals(PAYLOAD, parameters.get(model.payload)),
                    "Incorrect Pulse.payload bytes");
            require(Arrays.equals(PULSE_TAG, tag), "Incorrect Pulse tag");
            require(sent == OrderType.RECEIVE && model.reliable.equals(transport), "Incorrect Pulse order/transport");
            require(info.hasProducingFederate() && producer.equals(info.getProducingFederate()),
                    "Incorrect Pulse producing-federate metadata");
            require(!info.hasSentRegions(), "Unexpected Pulse regions");
            received = true;
            log("verified Pulse.payload (RO)");
        }
    }
}
