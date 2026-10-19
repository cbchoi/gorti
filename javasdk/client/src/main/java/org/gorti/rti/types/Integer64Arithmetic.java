package org.gorti.rti.types;

import hla.rti1516e.exceptions.IllegalTimeArithmetic;

final class Integer64Arithmetic {
    private Integer64Arithmetic() {}

    static long validValue(long value) {
        if (value < 0) {
            throw new IllegalArgumentException("Integer64 time values must be nonnegative");
        }
        return value;
    }

    static long add(long left, long right) throws IllegalTimeArithmetic {
        if (right > Long.MAX_VALUE - left) {
            throw new IllegalTimeArithmetic("Result exceeds the Integer64 time range");
        }
        return left + right;
    }

    static long subtract(long left, long right) throws IllegalTimeArithmetic {
        if (right > left) {
            throw new IllegalTimeArithmetic("Result is below the Integer64 time range");
        }
        return left - right;
    }
}
