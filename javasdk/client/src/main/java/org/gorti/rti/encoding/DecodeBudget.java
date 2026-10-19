package org.gorti.rti.encoding;

/** Shares an object-array entry budget across a complete nested decode operation. */
final class DecodeBudget {
    private static final ThreadLocal<DecodeBudget> ACTIVE = new ThreadLocal<>();
    private int remaining = EncodingSupport.MAX_ARRAY_ELEMENTS;
    private int depth;

    private DecodeBudget() { }

    static DecodeBudget enter() {
        DecodeBudget budget = ACTIVE.get();
        if (budget == null) {
            budget = new DecodeBudget();
            ACTIVE.set(budget);
        }
        budget.depth++;
        return budget;
    }

    static void reserve(int count) {
        DecodeBudget budget = ACTIVE.get();
        if (budget == null) throw new IllegalStateException("No active decode operation");
        if (count < 0 || count > budget.remaining) {
            throw new IllegalArgumentException("Nested arrays exceed the decode element budget");
        }
        budget.remaining -= count;
    }

    void close() {
        if (--depth == 0) ACTIVE.remove();
    }
}
