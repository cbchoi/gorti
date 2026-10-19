# IEEE Java API Material

Reprinted with permission from IEEE 1516.1(TM)-2010.

The files below `src/main/java/hla/rti1516e` originate from the official
`IEEE1516-2010_Java_API.zip` accompanying IEEE 1516.1-2010. Their original license
headers are retained. Those headers grant a general, royalty-free license to
copy, distribute, display and make derivative works with the attribution above.
This permission concerns the API material, not the licensed standard publication.

Source archive SHA-256:
`66485e053b6183df2df94e35de7c609677f2ccaef0830edde014aad066e41890`.

The archive supplies 204 Java source files. No commercial RTI implementation
or Portico implementation code is included. API definitions alone do not supply
an RTI implementation or establish conformance.

Two discovery helpers, `RtiFactoryFactory` and `LogicalTimeFactoryFactory`, use
`java.util.ServiceLoader` instead of `javax.imageio.spi.ServiceRegistry` in this
distribution. Java 11 rejects non-ImageIO service types in `ServiceRegistry`.
The public method signatures and attribution are unchanged. All other imported
API sources are unmodified. `ProviderTest` exercises both discovery paths.
