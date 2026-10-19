package org.gorti.rti.types;

import hla.rti1516e.exceptions.IllegalTimeArithmetic;

final strictfp class Float64Arithmetic {
    private Float64Arithmetic() {}

    static double add(double left, double right) throws IllegalTimeArithmetic {
        double result = left + right;
        // IEEE 1516.1 section 12.3 requires a nonzero interval to change the value.
        if (right != 0.0 && result == left) {
            result = Math.nextUp(left);
        }
        return checked(result);
    }

    static double subtract(double left, double right) throws IllegalTimeArithmetic {
        double result = left - right;
        if (right != 0.0 && result == left) {
            result = Math.nextDown(left);
        }
        return checked(result);
    }

    private static double checked(double result) throws IllegalTimeArithmetic {
        if (!Double.isFinite(result) || result < 0.0) {
            throw new IllegalTimeArithmetic("Result is outside the Float64 time range");
        }
        return result == 0.0 ? 0.0 : result;
    }
}
