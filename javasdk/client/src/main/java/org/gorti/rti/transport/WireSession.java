package org.gorti.rti.transport;

import com.google.protobuf.ByteString;
import com.google.protobuf.Descriptors.FieldDescriptor;
import com.google.protobuf.Message;
import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.ClientInterceptor;
import io.grpc.ClientInterceptors;
import io.grpc.Deadline;
import io.grpc.ForwardingClientCall;
import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import java.time.Duration;
import java.util.Objects;
import java.util.concurrent.TimeUnit;
import java.util.function.Consumer;
import rti.v1.Common;
import rti.v1.Stream;

/** Owns one connection and at most one joined federate's callback subscription. */
public final class WireSession implements AutoCloseable {
    private static final Duration DEFAULT_RPC_TIMEOUT = Duration.ofSeconds(10);
    private static final Duration DEFAULT_CLOSE_TIMEOUT = Duration.ofSeconds(5);
    private static final Metadata.Key<byte[]> SESSION_TOKEN =
            Metadata.Key.of("gorti-session-token-bin", Metadata.BINARY_BYTE_MARSHALLER);

    private final Object lock = new Object();
    private final ManagedChannel transport;
    private final Duration rpcTimeout;
    private final Duration closeTimeout;
    private Membership membership;
    private EventSubscription subscription;
    private boolean closed;

    /** The target is a gRPC channel target, normally host:port. TLS is the default. */
    public WireSession(String target, boolean plaintext) {
        this(newChannel(target, plaintext), DEFAULT_RPC_TIMEOUT, DEFAULT_CLOSE_TIMEOUT);
    }

    WireSession(ManagedChannel transport, Duration rpcTimeout, Duration closeTimeout) {
        this.transport = Objects.requireNonNull(transport, "transport");
        this.rpcTimeout = positive(rpcTimeout, "rpcTimeout");
        this.closeTimeout = positive(closeTimeout, "closeTimeout");
    }

    private static ManagedChannel newChannel(String target, boolean plaintext) {
        if (Objects.requireNonNull(target, "target").isBlank()) {
            throw new IllegalArgumentException("target must not be blank");
        }
        ManagedChannelBuilder<?> builder = ManagedChannelBuilder.forTarget(target);
        if (plaintext) {
            builder.usePlaintext();
        } else {
            builder.useTransportSecurity();
        }
        return builder.build();
    }

    /** Installs an immutable join identity and stops callbacks from the previous join. */
    public Channel setMembership(String federation, long handle, long generation, ByteString token) {
        Membership next = new Membership(federation, handle, generation, token);
        EventSubscription previous;
        synchronized (lock) {
            requireOpen();
            previous = subscription;
            subscription = null;
            membership = next;
        }
        if (previous != null) {
            previous.close();
        }
        return snapshotChannel(next);
    }

    /** Captures the current identity; a previously returned channel never adopts a later token. */
    public Channel channel() {
        synchronized (lock) {
            requireOpen();
            return snapshotChannel(requireMembership());
        }
    }

    /** Restore rekeys the existing authenticated stream; it must not unsubscribe it. */
    public void rebindMembership(long restoredHandle) {
        synchronized (lock) {
            requireOpen();
            Membership previous = requireMembership();
            membership = new Membership(previous.federation, restoredHandle, previous.generation, previous.token);
        }
    }

    /** For creation, listing, and joining. The caller supplies the join's generation field. */
    public Channel unauthenticatedChannel() {
        synchronized (lock) {
            requireOpen();
            return snapshotChannel(null);
        }
    }

    /** Returns only after the server's empty ready batch, never after a synthetic callback. */
    public EventSubscription subscribe(Consumer<Stream.FederateEvent> listener,
            Consumer<Throwable> errorListener) {
        return subscribe(listener, errorListener, rpcTimeout);
    }

    public EventSubscription subscribe(Consumer<Stream.FederateEvent> listener,
            Consumer<Throwable> errorListener, Duration readyTimeout) {
        positive(readyTimeout, "readyTimeout");
        EventSubscription next;
        synchronized (lock) {
            requireOpen();
            Membership current = requireMembership();
            if (subscription != null && !subscription.isTerminated()) {
                throw new IllegalStateException("callback subscription already open");
            }
            Stream.EventsRequest request = Stream.EventsRequest.newBuilder()
                    .setWireVersion(Common.WireVersion.WIRE_VERSION_V1)
                    .setFederationName(current.federation)
                    .setFederateHandle(current.handle)
                    .setExpectedFederationGeneration(current.generation).build();
            next = new EventSubscription(snapshotChannel(current), request, listener,
                    errorListener, closeTimeout);
            subscription = next;
        }
        try {
            next.start();
            next.awaitReady(readyTimeout);
            return next;
        } catch (RuntimeException failure) {
            try {
                next.close();
            } catch (RuntimeException teardownFailure) {
                failure.addSuppressed(teardownFailure);
            }
            synchronized (lock) {
                if (subscription == next) {
                    subscription = null;
                }
            }
            throw failure;
        }
    }

    public void unsubscribe() {
        EventSubscription previous;
        synchronized (lock) {
            previous = subscription;
            subscription = null;
        }
        if (previous != null) {
            previous.close();
        }
    }

    public void clearMembership() {
        EventSubscription previous;
        synchronized (lock) {
            membership = null;
            previous = subscription;
            subscription = null;
        }
        if (previous != null) {
            previous.close();
        }
    }

    @Override
    public void close() {
        EventSubscription previous;
        synchronized (lock) {
            if (closed) {
                return;
            }
            closed = true;
            membership = null;
            previous = subscription;
            subscription = null;
        }
        // Never hold the session lock while waiting for a callback to return.
        try {
            if (previous != null) {
                previous.close();
            }
        } finally {
            transport.shutdownNow();
            if (!EventSubscription.inCallback()) {
                try {
                    if (!transport.awaitTermination(closeTimeout.toNanos(), TimeUnit.NANOSECONDS)) {
                        throw new IllegalStateException("gRPC channel teardown timed out");
                    }
                } catch (InterruptedException interrupted) {
                    Thread.currentThread().interrupt();
                    throw new IllegalStateException("interrupted during channel teardown", interrupted);
                }
            }
        }
    }

    private Membership requireMembership() {
        if (membership == null) {
            throw new IllegalStateException("no federation membership");
        }
        return membership;
    }

    private void requireOpen() {
        if (closed) {
            throw new IllegalStateException("session is closed");
        }
    }

    static Duration positive(Duration duration, String name) {
        Objects.requireNonNull(duration, name);
        if (duration.isZero() || duration.isNegative()) {
            throw new IllegalArgumentException(name + " must be positive");
        }
        duration.toNanos();
        return duration;
    }

    private Channel snapshotChannel(Membership snapshot) {
        return ClientInterceptors.intercept(transport, new ClientInterceptor() {
            @Override
            public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
                    MethodDescriptor<ReqT, RespT> method, CallOptions options, Channel next) {
                // Callback streams live until unsubscribe; only their readiness wait is bounded.
                if (!method.getFullMethodName().equals("rti.v1.StreamService/EventBatches")) {
                    Deadline bound = Deadline.after(rpcTimeout.toNanos(), TimeUnit.NANOSECONDS);
                    Deadline supplied = options.getDeadline();
                    options = options.withDeadline(supplied == null ? bound : supplied.minimum(bound));
                }
                return new ForwardingClientCall.SimpleForwardingClientCall<ReqT, RespT>(
                        next.newCall(method, options)) {
                    @Override
                    public void start(Listener<RespT> responseListener, Metadata headers) {
                        headers.discardAll(SESSION_TOKEN);
                        if (snapshot != null) {
                            headers.put(SESSION_TOKEN, snapshot.token.toByteArray());
                        }
                        super.start(responseListener, headers);
                    }

                    @Override
                    @SuppressWarnings("unchecked")
                    public void sendMessage(ReqT request) {
                        super.sendMessage(snapshot == null ? request : (ReqT) snapshot.fence(request));
                    }
                };
            }
        });
    }

    private static final class Membership {
        final String federation;
        final long handle;
        final long generation;
        final ByteString token;

        Membership(String federation, long handle, long generation, ByteString token) {
            this.federation = Objects.requireNonNull(federation, "federation");
            this.token = Objects.requireNonNull(token, "token");
            if (federation.isBlank() || handle == 0 || token.isEmpty()) {
                throw new IllegalArgumentException("federation, nonzero handle, and token are required");
            }
            this.handle = handle;
            this.generation = generation;
        }

        Object fence(Object request) {
            if (!(request instanceof Message)) {
                return request;
            }
            Message message = (Message) request;
            FieldDescriptor generationField = message.getDescriptorForType()
                    .findFieldByName("expected_federation_generation");
            if (generationField == null) {
                return request;
            }
            FieldDescriptor federationField = message.getDescriptorForType().findFieldByName("federation_name");
            if (federationField == null || !federation.equals(message.getField(federationField))) {
                throw new IllegalArgumentException("generation-fenced request belongs to another federation");
            }
            if (message.hasField(generationField)) {
                if (!Long.valueOf(generation).equals(message.getField(generationField))) {
                    throw new IllegalArgumentException("request generation differs from session snapshot");
                }
                return request;
            }
            return message.toBuilder().setField(generationField, generation).build();
        }
    }
}
