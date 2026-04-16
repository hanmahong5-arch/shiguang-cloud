# Generate NCSoft password hash test vectors from the canonical C# algorithm.
# Source: AionNetGate/AionNetGate/Services/AccountService.cs:533-590
# Usage: powershell -ExecutionPolicy Bypass -File gen_test_vectors.ps1 > vectors.txt

$csharp = @'
using System;
using System.Text;

public static class NCSoftHash
{
    public static string EncodePasswordHash(string password)
    {
        return "0x" + BitConverter.ToString(GetAccountPasswordHash(password), 0).Replace("-", string.Empty).ToUpper();
    }

    private static byte[] GetAccountPasswordHash(string input)
    {
        byte[] buffer = new byte[0x11];
        byte[] src = new byte[0x11];
        byte[] bytes = Encoding.ASCII.GetBytes(input);
        for (int i = 0; i < input.Length; i++)
        {
            buffer[i + 1] = bytes[i];
            src[i + 1] = buffer[i + 1];
        }
        long num = ((buffer[1] + (buffer[2] * 0x100L)) + (buffer[3] * 0x10000L)) + (buffer[4] * 0x1000000);
        long num2 = (num * 0x3407fL) + 0x269735L;
        num2 -= (num2 / 0x100000000L) * 0x100000000L;
        num = ((buffer[5] + (buffer[6] * 0x100)) + (buffer[7] * 0x10000L)) + (buffer[8] * 0x1000000);
        long num3 = (num * 0x340ff) + 0x269741;
        num3 -= (num3 / 0x100000000) * 0x100000000;
        num = ((buffer[9] + (buffer[10] * 0x100L)) + (buffer[11] * 0x10000L)) + (buffer[12] * 0x1000000);
        long num4 = (num * 0x340d3) + 0x269935;
        num4 -= (num4 / 0x100000000) * 0x100000000L;
        num = ((buffer[13] + (buffer[14] * 0x100)) + (buffer[15] * 0x10000L)) + (buffer[0x10] * 0x1000000);
        long num5 = (num * 0x3433d) + 0x269acdL;
        num5 -= (num5 / 0x100000000) * 0x100000000;
        buffer[4] = (byte)(num2 / 0x1000000L);
        buffer[3] = (byte)((num2 - (buffer[4] * 0x1000000)) / 0x10000L);
        buffer[2] = (byte)((num2 - (buffer[4] * 0x1000000) - (buffer[3] * 0x10000)) / 0x100L);
        buffer[1] = (byte)(num2 - (buffer[4] * 0x1000000) - (buffer[3] * 0x10000) - (buffer[2] * 0x100));
        buffer[8] = (byte)(num3 / 0x1000000L);
        buffer[7] = (byte)((num3 - (buffer[8] * 0x1000000L)) / 0x10000L);
        buffer[6] = (byte)((num3 - (buffer[8] * 0x1000000L) - (buffer[7] * 0x10000)) / 0x100L);
        buffer[5] = (byte)(num3 - (buffer[8] * 0x1000000L) - (buffer[7] * 0x10000) - (buffer[6] * 0x100));
        buffer[12] = (byte)(num4 / 0x1000000L);
        buffer[11] = (byte)((num4 - (buffer[12] * 0x1000000L)) / (0x10000L));
        buffer[10] = (byte)((num4 - (buffer[12] * 0x1000000L) - (buffer[11] * 0x10000)) / 0x100);
        buffer[9] = (byte)(num4 - (buffer[12] * 0x1000000L) - (buffer[11] * 0x10000) - (buffer[10] * 0x100));
        buffer[0x10] = (byte)(num5 / 0x1000000L);
        buffer[15] = (byte)((num5 - (buffer[0x10] * 0x1000000L)) / 0x10000L);
        buffer[14] = (byte)((num5 - (buffer[0x10] * 0x1000000L) - (buffer[15] * 0x10000)) / 0x100);
        buffer[13] = (byte)(num5 - (buffer[0x10] * 0x1000000L) - (buffer[15] * 0x10000) - (buffer[14] * 0x100));
        src[1] = (byte)(src[1] ^ buffer[1]);
        int index = 1;
        while (index < 0x10)
        {
            index++;
            src[index] = (byte)((src[index] ^ src[index - 1]) ^ buffer[index]);
        }
        index = 0;
        while (index < 0x10)
        {
            index++;
            if (src[index] == 0)
            {
                src[index] = 0x66;
            }
        }
        byte[] dst = new byte[0x10];
        Buffer.BlockCopy(src, 1, dst, 0, 0x10);
        return dst;
    }
}
'@

Add-Type -TypeDefinition $csharp -Language CSharp

$inputs = @(
    "",
    "a",
    "admin",
    "test",
    "password",
    "123456",
    "aion",
    "1",
    "abcdefgh",
    "ABC123",
    "9876543210",
    "shiguangAI",
    "LongerPassw",
    "Max16Characters1",
    "p@ssw0rd!"
)

foreach ($s in $inputs) {
    $hash = [NCSoftHash]::EncodePasswordHash($s)
    Write-Output "$s`t$hash"
}
