package org.gorti.examples;

import hla.rti1516e.CallbackModel;
import hla.rti1516e.LogicalTime;
import hla.rti1516e.NullFederateAmbassador;
import hla.rti1516e.RTIambassador;
import hla.rti1516e.ResignAction;
import hla.rti1516e.RtiFactoryFactory;
import hla.rti1516e.time.HLAfloat64Time;
import hla.rti1516e.time.HLAfloat64TimeFactory;
import java.net.URL;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Duration;
import java.util.Locale;
import java.util.UUID;
import java.util.concurrent.TimeoutException;
import java.util.function.BooleanSupplier;

/** A single-federate diagnostic, not a two-federate time-barrier experiment. */
public final class TimeFederate {
    private TimeFederate() {}

    public static void main(String[] args) throws Exception {
        String endpoint = System.getProperty("gorti.endpoint", "");
        if (endpoint.isBlank()) {
            throw new IllegalArgumentException("Set gorti.endpoint to grpc://host:port or grpcs://host:port");
        }
        String configuredFom = System.getProperty("gorti.fom", "");
        Path fom = configuredFom.isBlank() ? defaultFom() : Path.of(configuredFom).toAbsolutePath();
        if (!Files.isRegularFile(fom)) {
            throw new IllegalArgumentException("FOM file not found: " + fom);
        }
        int timeoutSeconds = Integer.parseInt(System.getProperty("gorti.timeoutSeconds", "10"));
        if (timeoutSeconds < 1 || timeoutSeconds > 300) {
            throw new IllegalArgumentException("gorti.timeoutSeconds must be between 1 and 300");
        }
        Duration timeout = Duration.ofSeconds(timeoutSeconds);
        String federation = "java-time-" + UUID.randomUUID();
        RTIambassador rti = RtiFactoryFactory.getRtiFactory("gorti").getRtiAmbassador();
        Callbacks callbacks = new Callbacks();
        boolean connected = false;
        boolean created = false;
        boolean joined = false;
        Exception failure = null;
        try {
            System.out.println("Single-federate time diagnostic (not a peer-wait test)");
            rti.connect(callbacks, CallbackModel.HLA_EVOKED, endpoint);
            connected = true;
            System.out.println("connected " + endpoint);
            rti.createFederationExecution(federation, new URL[] {fom.toUri().toURL()}, HLAfloat64TimeFactory.NAME);
            created = true;
            System.out.println("created " + federation);
            rti.joinFederationExecution("time-diagnostic", "TimeFederate", federation);
            joined = true;
            System.out.println("joined time-diagnostic");
            if (!(rti.getTimeFactory() instanceof HLAfloat64TimeFactory)) {
                throw new IllegalStateException("Federation did not provide HLAfloat64Time");
            }
            HLAfloat64TimeFactory time = (HLAfloat64TimeFactory) rti.getTimeFactory();
            rti.enableTimeRegulation(time.makeInterval(1.0));
            await(rti, callbacks, () -> callbacks.regulating, "timeRegulationEnabled", timeout);
            rti.enableTimeConstrained();
            await(rti, callbacks, () -> callbacks.constrained, "timeConstrainedEnabled", timeout);
            for (int target = 1; target <= 3; target++) {
                callbacks.grant = Double.NaN;
                System.out.printf(Locale.ROOT, "TAR %.1f%n", (double) target);
                rti.timeAdvanceRequest(time.makeTime(target));
                await(rti, callbacks, () -> !Double.isNaN(callbacks.grant), "timeAdvanceGrant", timeout);
                if (Double.compare(callbacks.grant, target) != 0) {
                    throw new IllegalStateException("Expected grant " + target + ", received " + callbacks.grant);
                }
            }
        } catch (Exception error) {
            failure = error;
        } finally {
            if (joined) {
                failure = cleanup(failure, "resigned", () -> rti.resignFederationExecution(ResignAction.NO_ACTION));
            }
            if (created) {
                failure = cleanup(failure, "destroyed " + federation, () -> rti.destroyFederationExecution(federation));
            }
            if (connected) {
                failure = cleanup(failure, "disconnected", rti::disconnect);
            }
        }
        if (failure != null) {
            throw failure;
        }
    }

    private static Path defaultFom() {
        for (Path directory = Path.of("").toAbsolutePath(); directory != null; directory = directory.getParent()) {
            Path candidate = directory.resolve("examples/go-tar-wait/tar-wait-fom.xml");
            if (Files.isRegularFile(candidate)) {
                return candidate;
            }
        }
        throw new IllegalArgumentException("Cannot locate the example FOM; set gorti.fom to its path");
    }

    private static void await(RTIambassador rti, Callbacks callbacks, BooleanSupplier completed,
            String event, Duration timeout) throws Exception {
        long started = System.nanoTime();
        while (!completed.getAsBoolean()) {
            if (callbacks.connectionFailure != null) {
                throw new IllegalStateException("Connection lost: " + callbacks.connectionFailure);
            }
            if (Thread.currentThread().isInterrupted()) {
                throw new InterruptedException("Interrupted while waiting for " + event);
            }
            long remaining = timeout.toNanos() - (System.nanoTime() - started);
            if (remaining <= 0) {
                throw new TimeoutException("Timed out waiting for " + event);
            }
            rti.evokeCallback(Math.min(0.1, remaining / 1_000_000_000.0));
        }
    }

    private static Exception cleanup(Exception prior, String description, Cleanup action) {
        try {
            action.run();
            System.out.println(description);
        } catch (Exception failure) {
            System.err.println("Cleanup failed (" + description + "): " + failure.getMessage());
            if (prior == null) {
                return failure;
            }
            prior.addSuppressed(failure);
        }
        return prior;
    }

    @FunctionalInterface
    private interface Cleanup {
        void run() throws Exception;
    }

    private static final class Callbacks extends NullFederateAmbassador {
        boolean regulating;
        boolean constrained;
        double grant = Double.NaN;
        String connectionFailure;

        @Override
        public void timeRegulationEnabled(LogicalTime time) {
            System.out.printf(Locale.ROOT, "timeRegulationEnabled %.1f%n", value(time));
            regulating = true;
        }

        @Override
        public void timeConstrainedEnabled(LogicalTime time) {
            System.out.printf(Locale.ROOT, "timeConstrainedEnabled %.1f%n", value(time));
            constrained = true;
        }

        @Override
        public void timeAdvanceGrant(LogicalTime time) {
            grant = value(time);
            System.out.printf(Locale.ROOT, "TAG %.1f%n", grant);
        }

        @Override
        public void connectionLost(String description) {
            connectionFailure = description;
        }

        private static double value(LogicalTime time) {
            if (!(time instanceof HLAfloat64Time)) {
                throw new IllegalArgumentException("Expected HLAfloat64Time callback");
            }
            return ((HLAfloat64Time) time).getValue();
        }
    }
}
