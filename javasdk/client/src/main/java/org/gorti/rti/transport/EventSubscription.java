package org.gorti.rti.transport;

import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.Metadata;
import io.grpc.Status;
import java.time.Duration;
import java.util.Objects;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.CompletionStage;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.function.Consumer;
import rti.v1.Stream;
import rti.v1.StreamServiceGrpc;

/** An ordered, explicitly acknowledged EventBatches subscription. */
public final class EventSubscription implements AutoCloseable {
    private static final ThreadLocal<Boolean> CALLBACK = ThreadLocal.withInitial(() -> false);
    private final ClientCall<Stream.EventsRequest, Stream.FederateEventBatch> call;
    private final Stream.EventsRequest request;
    private final Consumer<Stream.FederateEvent> listener;
    private final Consumer<Throwable> errorListener;
    private final Duration closeTimeout;
    private final CompletableFuture<Void> ready = new CompletableFuture<>();
    private final CompletableFuture<Void> ended = new CompletableFuture<>();
    private final CompletableFuture<Void> terminated = new CompletableFuture<>();
    private final AtomicBoolean cancelled = new AtomicBoolean();
    private final AtomicBoolean failed = new AtomicBoolean();
    private volatile boolean acknowledged;

    EventSubscription(Channel channel, Stream.EventsRequest request,
            Consumer<Stream.FederateEvent> listener, Consumer<Throwable> errorListener,
            Duration closeTimeout) {
        this.call = channel.newCall(StreamServiceGrpc.getEventBatchesMethod(), CallOptions.DEFAULT);
        this.request = request;
        this.listener = Objects.requireNonNull(listener, "listener");
        this.errorListener = Objects.requireNonNull(errorListener, "errorListener");
        this.closeTimeout = closeTimeout;
    }

    void start() {
        call.start(new ClientCall.Listener<Stream.FederateEventBatch>() {
            @Override
            public void onMessage(Stream.FederateEventBatch batch) {
                boolean previous = CALLBACK.get();
                CALLBACK.set(true);
                try {
                    if (cancelled.get() || failed.get()) {
                        return;
                    }
                    if (!acknowledged) {
                        if (!batch.getReady() || batch.getEventsCount() != 0) {
                            fail(Status.DATA_LOSS.withDescription("missing empty callback ready batch")
                                    .asRuntimeException());
                            return;
                        }
                        acknowledged = true;
                        ready.complete(null);
                    } else if (batch.getReady()) {
                        fail(Status.DATA_LOSS.withDescription("duplicate callback ready batch")
                                .asRuntimeException());
                    } else {
                        for (Stream.FederateEvent event : batch.getEventsList()) {
                            if (cancelled.get()) {
                                break;
                            }
                            listener.accept(event);
                        }
                    }
                } catch (Throwable failure) {
                    fail(failure);
                } finally {
                    CALLBACK.set(previous);
                    if (!cancelled.get() && !failed.get()) {
                        call.request(1);
                    }
                }
            }

            @Override
            public void onClose(Status status, Metadata trailers) {
                boolean previous = CALLBACK.get();
                CALLBACK.set(true);
                try {
                    if (cancelled.get()) {
                        ready.completeExceptionally(Status.CANCELLED.asRuntimeException());
                        ended.complete(null);
                    } else {
                        Throwable failure = (status.isOk()
                                ? Status.UNAVAILABLE.withDescription("callback stream ended") : status)
                                .asRuntimeException(trailers);
                        reportFailure(failure);
                    }
                } finally {
                    terminated.complete(null);
                    CALLBACK.set(previous);
                }
            }
        }, new Metadata());
        if (cancelled.get()) {
            call.cancel("callback subscription closed", null);
            return;
        }
        call.request(1);
        call.sendMessage(request);
        call.halfClose();
    }

    /** Completes exceptionally for protocol, server, transport, or listener failures. */
    public CompletionStage<Void> completion() {
        return ended.minimalCompletionStage();
    }

    public boolean isTerminated() {
        return terminated.isDone();
    }

    public void awaitReady(Duration timeout) {
        WireSession.positive(timeout, "timeout");
        try {
            ready.get(timeout.toNanos(), TimeUnit.NANOSECONDS);
        } catch (TimeoutException timeoutFailure) {
            cancel();
            throw Status.DEADLINE_EXCEEDED.withDescription("callback ready handshake timed out")
                    .withCause(timeoutFailure).asRuntimeException();
        } catch (InterruptedException interrupted) {
            cancel();
            Thread.currentThread().interrupt();
            throw Status.CANCELLED.withCause(interrupted).asRuntimeException();
        } catch (ExecutionException failure) {
            Throwable cause = failure.getCause();
            if (cause instanceof RuntimeException) {
                throw (RuntimeException) cause;
            }
            throw Status.UNKNOWN.withCause(cause).asRuntimeException();
        }
    }

    private void fail(Throwable failure) {
        // Cancel before invoking application code, which may close or rejoin the session.
        call.cancel("callback subscription failed", failure);
        reportFailure(failure);
    }

    private void reportFailure(Throwable failure) {
        if (!failed.compareAndSet(false, true)) {
            return;
        }
        ready.completeExceptionally(failure);
        ended.completeExceptionally(failure);
        try {
            errorListener.accept(failure);
        } catch (Throwable listenerFailure) {
            if (listenerFailure != failure) {
                failure.addSuppressed(listenerFailure);
            }
        }
    }

    void cancel() {
        if (cancelled.compareAndSet(false, true)) {
            ready.completeExceptionally(Status.CANCELLED.asRuntimeException());
            call.cancel("callback subscription closed", null);
        }
    }

    static boolean inCallback() {
        return CALLBACK.get();
    }

    @Override
    public void close() {
        cancel();
        if (inCallback()) {
            return;
        }
        try {
            terminated.get(closeTimeout.toNanos(), TimeUnit.NANOSECONDS);
        } catch (ExecutionException expectedFailure) {
            // Already surfaced through completion() and errorListener.
        } catch (TimeoutException timeout) {
            throw new IllegalStateException("callback stream teardown timed out", timeout);
        } catch (InterruptedException interrupted) {
            Thread.currentThread().interrupt();
            throw new IllegalStateException("interrupted during callback teardown", interrupted);
        }
    }
}
