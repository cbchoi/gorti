package org.gorti.rti;

import com.google.protobuf.ByteString;
import hla.rti1516e.exceptions.RTIexception;
import hla.rti1516e.exceptions.RTIinternalError;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.Map;
import java.util.List;
import java.io.IOException;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import com.google.protobuf.Message;
import com.google.protobuf.Descriptors.FieldDescriptor;
import java.util.function.BooleanSupplier;
import rti.v1.Stream;

/** Opaque receipts identify attempts independently of restored federate handles. */
final class CallbackInvocationDelivery {
    interface Reporter {
        void report(Stream.FederateEvent event, boolean success, String exception, boolean entry) throws RTIexception;
    }
    interface Admission { void prepare() throws RTIexception; }
    private static final int LIMIT = 8192;
    private final Reporter reporter;
    private final BooleanSupplier entrySupported;
    private final Map<ByteString, Attempt> attempts = new HashMap<>();
    private final ArrayDeque<ByteString> completed = new ArrayDeque<>();
    private final Map<String, Outcome> outcomes = new HashMap<>();

    static final class Outcome {
        final String identity;
        final boolean success;
        final String exception;
        Outcome(String identity, boolean success, String exception) throws IOException {
            if (identity == null || !identity.matches("[0-9a-f]{64}") || exception == null
                    || success && !exception.isEmpty() || !success && exception.isEmpty())
                throw new IOException("Invalid completed callback outcome");
            this.identity = identity; this.success = success; this.exception = exception;
        }
    }

    static String identity(Stream.FederateEvent event) throws RTIinternalError {
        Message metadata = recovery(event);
        ByteString bytes;
        if (metadata != null) bytes = (ByteString) metadata.getField(metadata.getDescriptorForType().findFieldByName("invocation_identity"));
        else {
            if (event.getCallbackReceipt().size() != 32) throw new RTIinternalError("Invalid callback receipt length");
            try { bytes = ByteString.copyFrom(MessageDigest.getInstance("SHA-256").digest(event.getCallbackReceipt().toByteArray())); }
            catch (NoSuchAlgorithmException impossible) { throw new IllegalStateException(impossible); }
        }
        if (bytes.size() != 32) throw new RTIinternalError("Invalid callback recovery identity");
        StringBuilder hex = new StringBuilder(64);
        for (byte value : bytes.toByteArray()) hex.append(String.format(java.util.Locale.ROOT, "%02x", value & 255));
        return hex.toString();
    }

    private static Message recovery(Stream.FederateEvent event) {
        FieldDescriptor field = event.getDescriptorForType().findFieldByName("callback_recovery");
        return field != null && event.hasField(field) ? (Message) event.getField(field) : null;
    }

    static boolean isRecovered(Stream.FederateEvent event) { return recovery(event) != null; }
    static boolean isOutcomeOnly(Stream.FederateEvent event) {
        Message metadata = recovery(event);
        return metadata != null && flag(metadata, "outcome_only");
    }

    private static boolean flag(Message metadata, String name) {
        FieldDescriptor field = metadata.getDescriptorForType().findFieldByName(name);
        return field != null && (Boolean) metadata.getField(field);
    }

    boolean recover(Stream.FederateEvent event) throws RTIexception {
        Message metadata = recovery(event);
        if (metadata == null) return false;
        if (!metadata.getUnknownFields().asMap().isEmpty()) throw new RTIinternalError("Unknown callback recovery fields");
        String identity = identity(event);
        boolean only = flag(metadata, "outcome_only"), done = flag(metadata, "completed");
        if (done) {
            if (!only || !event.getCallbackReceipt().isEmpty() || event.getEventCase() != Stream.FederateEvent.EventCase.EVENT_NOT_SET)
                throw new RTIinternalError("Invalid completed callback reconciliation marker");
            synchronized (this) {
                outcomes.remove(identity);
                attempts.entrySet().removeIf(entry -> identity.equals(entry.getValue().identity));
                completed.removeIf(receipt -> !attempts.containsKey(receipt));
            }
            return true;
        }
        if (event.getCallbackReceipt().size() != 32 || event.getEventCase() == Stream.FederateEvent.EventCase.EVENT_NOT_SET
                || event.hasHalted() || event.hasLocalValidationBarrier()) throw new RTIinternalError("Invalid recovery callback carrier");
        if (!only) return false;
        synchronized (this) {
            if (!attempts.containsKey(event.getCallbackReceipt())) {
                if (attempts.size() - completed.size() >= LIMIT)
                    throw new RTIinternalError("Pending callback invocation capacity exceeded");
                Outcome outcome = outcomes.get(identity);
                if (outcome == null) throw new RTIinternalError("No known completed outcome for recovery callback");
                Attempt attempt = new Attempt(event, identity);
                attempt.finished = true; attempt.success = outcome.success; attempt.exception = outcome.exception;
                attempts.put(event.getCallbackReceipt(), attempt);
            }
        }
        flush();
        return true;
    }

    synchronized List<Outcome> checkpointOutcomes() throws IOException {
        for (Attempt attempt : attempts.values()) if (!attempt.finished)
            throw new IOException("Cannot checkpoint a running callback body");
        if (outcomes.size() > LIMIT) throw new IOException("Pending callback outcome capacity exceeded");
        ArrayList<Outcome> saved = new ArrayList<>(outcomes.values());
        saved.sort(java.util.Comparator.comparing(value -> value.identity));
        return List.copyOf(saved);
    }

    synchronized void restoreOutcomes(List<Outcome> saved) {
        outcomes.clear();
        for (Outcome outcome : saved) outcomes.put(outcome.identity, outcome);
    }

    CallbackInvocationDelivery(Reporter reporter, BooleanSupplier entrySupported) {
        this.reporter = reporter;
        this.entrySupported = entrySupported;
    }

    boolean begin(Stream.FederateEvent event) throws RTIexception {
        return begin(event, () -> { });
    }

    boolean begin(Stream.FederateEvent event, Admission admission) throws RTIexception {
        if (recover(event)) return false;
        ByteString receipt = event.getCallbackReceipt();
        if (receipt.isEmpty()) { admission.prepare(); return true; }
        String identity = identity(event);
        boolean duplicate;
        synchronized (this) {
            duplicate = attempts.containsKey(receipt);
            if (!duplicate) {
                if (attempts.size() - completed.size() >= LIMIT)
                    throw new RTIinternalError("Pending callback invocation capacity exceeded");
                attempts.put(receipt, new Attempt(event, identity));
            }
        }
        if (duplicate) { flush(); return false; }
        try {
            admission.prepare();
            if (entrySupported.getAsBoolean()) reporter.report(event, true, "", true);
        }
        catch (RTIexception | RuntimeException | Error failure) {
            synchronized (this) { attempts.remove(receipt); }
            throw failure;
        }
        return true;
    }

    void complete(Stream.FederateEvent event, Throwable failure) throws RTIexception {
        if (event.getCallbackReceipt().isEmpty()) return;
        synchronized (this) {
            Attempt attempt = attempts.get(event.getCallbackReceipt());
            if (attempt == null) return;
            attempt.success = failure == null;
            attempt.exception = failure == null ? "" : failure.getClass().getSimpleName();
            attempt.finished = true;
            try { outcomes.put(attempt.identity, new Outcome(attempt.identity, attempt.success, attempt.exception)); }
            catch (IOException invalid) { throw new RTIinternalError("Cannot retain callback outcome", invalid); }
        }
        flush();
    }

    synchronized void entryFailed(Stream.FederateEvent event) {
        Attempt attempt = attempts.get(event.getCallbackReceipt());
        if (attempt != null && !attempt.finished) attempts.remove(event.getCallbackReceipt());
    }

    synchronized void restoreEpoch() {
        attempts.clear();
        completed.clear();
        outcomes.clear();
    }

    void flush() throws RTIexception {
        ArrayList<Attempt> pending = new ArrayList<>();
        synchronized (this) {
            for (Attempt attempt : attempts.values()) if (attempt.finished && !attempt.reported) pending.add(attempt);
        }
        RTIexception failure = null;
        for (Attempt attempt : pending) {
            synchronized (this) {
                if (attempts.get(attempt.event.getCallbackReceipt()) != attempt) continue;
            }
            try { reporter.report(attempt.event, attempt.success, attempt.exception, false); }
            catch (RTIexception rejected) {
                if (failure == null) failure = rejected; else failure.addSuppressed(rejected);
                continue;
            }
            synchronized (this) {
                if (attempts.get(attempt.event.getCallbackReceipt()) != attempt) continue;
                if (!attempt.reported) {
                    attempt.reported = true;
                    outcomes.remove(attempt.identity);
                    completed.add(attempt.event.getCallbackReceipt());
                    if (completed.size() > LIMIT) attempts.remove(completed.remove());
                }
            }
        }
        if (failure != null) throw failure;
    }

    private static final class Attempt {
        final Stream.FederateEvent event;
        final String identity;
        boolean finished, success, reported;
        String exception;
        Attempt(Stream.FederateEvent event, String identity) { this.event = event; this.identity = identity; }
    }
}
