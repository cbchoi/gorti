package org.gorti.rti;

import hla.rti1516e.exceptions.RTIexception;
import hla.rti1516e.exceptions.RTIinternalError;
import io.grpc.Metadata;
import io.grpc.StatusRuntimeException;
import java.util.function.Supplier;

final class WireErrors {
    private static final Metadata.Key<String> EXCEPTION =
            Metadata.Key.of("rti-spec-exception", Metadata.ASCII_STRING_MARSHALLER);

    private WireErrors() { }

    static <T> T call(Supplier<T> action) throws RTIexception {
        try { return action.get(); }
        catch (StatusRuntimeException failure) { throw translate(failure); }
    }

    static RTIexception translate(StatusRuntimeException failure) {
        String name = failure.getTrailers() == null ? null : failure.getTrailers().get(EXCEPTION);
        if (name != null && name.matches("[A-Za-z][A-Za-z0-9]*")) {
            try {
                Class<? extends RTIexception> type = Class.forName("hla.rti1516e.exceptions." + name)
                        .asSubclass(RTIexception.class);
                return type.getConstructor(String.class, Throwable.class)
                        .newInstance(failure.getStatus().toString(), failure);
            } catch (ReflectiveOperationException | ClassCastException ignored) {
                // Unknown server exception names must not masquerade as successful calls.
            }
        }
        return new RTIinternalError(failure.getStatus().toString(), failure);
    }
}
