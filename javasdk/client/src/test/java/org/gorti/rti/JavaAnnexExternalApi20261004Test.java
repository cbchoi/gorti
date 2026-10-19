package org.gorti.rti;

import hla.rti1516e.RTIambassador;
import java.io.StringWriter;
import java.lang.reflect.*;
import java.net.URLClassLoader;
import java.nio.file.*;
import java.security.MessageDigest;
import java.util.*;
import java.util.stream.Collectors;
import java.util.zip.ZipFile;
import javax.tools.ToolProvider;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.condition.EnabledIfSystemProperty;
import org.junit.jupiter.api.io.TempDir;

import static org.junit.jupiter.api.Assertions.*;

@EnabledIfSystemProperty(named = "gorti.ieee.java.archive", matches = ".+",
        disabledReason = "Requires the separately supplied IEEE Java source archive")
class JavaAnnexExternalApi20261004Test {
    @TempDir Path temporary;

    @Test
    void loadedPublicProtectedApiMatchesAll204DeclarationsInPinnedIeeeArchive() throws Exception {
        Path archive = Path.of(System.getProperty("gorti.ieee.java.archive"));
        byte[] digest = MessageDigest.getInstance("SHA-256").digest(Files.readAllBytes(archive));
        StringBuilder hex = new StringBuilder();
        for (byte value : digest) hex.append(String.format("%02x", value));
        assertEquals("66485e053b6183df2df94e35de7c609677f2ccaef0830edde014aad066e41890", hex.toString());
        List<String> sources = new ArrayList<>();
        Set<String> names = new TreeSet<>();
        try (ZipFile zip = new ZipFile(archive.toFile())) {
            for (var entry : Collections.list(zip.entries())) {
                if (!entry.getName().startsWith("java/src/hla/") || !entry.getName().endsWith(".java")) continue;
                String relative = entry.getName().substring("java/src/".length());
                Path target = temporary.resolve(relative).normalize();
                assertTrue(target.startsWith(temporary));
                Files.createDirectories(target.getParent());
                try (var input = zip.getInputStream(entry)) { Files.copy(input, target); }
                sources.add(target.toString());
                names.add(relative.substring(0, relative.length() - 5).replace('/', '.'));
            }
        }
        assertEquals(204, names.size());
        Path compiled = Files.createDirectory(temporary.resolve("original-classes"));
        var compiler = ToolProvider.getSystemJavaCompiler();
        assertNotNull(compiler, "The independent archive must be compiled with a JDK");
        StringWriter diagnostics = new StringWriter();
        try (var manager = compiler.getStandardFileManager(null, null, null)) {
            assertTrue(compiler.getTask(diagnostics, manager, null,
                    List.of("--release", "11", "-d", compiled.toString()), null,
                    manager.getJavaFileObjectsFromStrings(sources)).call(), diagnostics.toString());
        }
        Path loadedRoot = Path.of(RTIambassador.class.getProtectionDomain().getCodeSource().getLocation().toURI());
        Set<String> loadedNames;
        if (Files.isDirectory(loadedRoot)) {
            try (var paths = Files.walk(loadedRoot.resolve("hla/rti1516e"))) {
                loadedNames = paths.filter(p -> p.toString().endsWith(".class") && !p.getFileName().toString().contains("$"))
                        .map(p -> loadedRoot.relativize(p).toString().replace('\\', '.').replace('/', '.'))
                        .map(n -> n.substring(0, n.length() - 6)).collect(Collectors.toCollection(TreeSet::new));
            }
        } else {
            try (ZipFile zip = new ZipFile(loadedRoot.toFile())) {
                loadedNames = zip.stream().map(e -> e.getName())
                        .filter(n -> n.startsWith("hla/rti1516e/") && n.endsWith(".class") && !n.contains("$"))
                        .map(n -> n.substring(0, n.length() - 6).replace('/', '.'))
                        .collect(Collectors.toCollection(TreeSet::new));
            }
        }
        assertEquals(names, loadedNames, "No missing, renamed, or extra top-level API declarations");
        // Platform parent prevents delegation to the provider's adapted API helpers.
        try (URLClassLoader original = new URLClassLoader(new java.net.URL[]{compiled.toUri().toURL()},
                ClassLoader.getPlatformClassLoader())) {
            for (String name : names) {
                assertEquals(declarations(Class.forName(name, false, original)),
                        declarations(Class.forName(name, false, RTIambassador.class.getClassLoader())), name);
            }
        }
        System.out.println("Independent pinned archive: 204 loaded top-level declarations and public/protected members matched; helper implementations and service semantics not asserted.");
    }

    private static SortedSet<String> declarations(Class<?> type) {
        SortedSet<String> result = new TreeSet<>();
        result.add(type.toGenericString());
        result.add("super " + type.getGenericSuperclass());
        for (Type parent : type.getGenericInterfaces()) result.add("interface " + parent.getTypeName());
        for (Method method : type.getDeclaredMethods()) if (visible(method.getModifiers())) result.add(method.toGenericString());
        for (Constructor<?> constructor : type.getDeclaredConstructors()) if (visible(constructor.getModifiers())) result.add(constructor.toGenericString());
        for (Field field : type.getDeclaredFields()) if (visible(field.getModifiers())) result.add(field.toGenericString());
        for (Class<?> nested : type.getDeclaredClasses()) if (visible(nested.getModifiers())) result.addAll(declarations(nested));
        return result;
    }

    private static boolean visible(int modifiers) {
        return Modifier.isPublic(modifiers) || Modifier.isProtected(modifiers);
    }
}
