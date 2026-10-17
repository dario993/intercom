package app.intercom.desktop.audio

import javax.sound.sampled.AudioFormat
import javax.sound.sampled.AudioSystem
import javax.sound.sampled.DataLine
import javax.sound.sampled.Line
import javax.sound.sampled.SourceDataLine
import javax.sound.sampled.TargetDataLine

/** 48 kHz, mono, signed 16 bit little-endian: the intercom's only PCM format. */
val PCM_FORMAT = AudioFormat(SAMPLE_RATE.toFloat(), 16, 1, true, false)
const val FRAME_BYTES = FRAME_SAMPLES * 2

/** A line opened on the requested device, or on the Windows default when that device is gone ([fellBack]). */
class Opened<T : Line>(val line: T, val device: String, val fellBack: Boolean)

/** Sound cards as Windows names them (Java Sound mixers), filtered to those that handle [PCM_FORMAT]. */
object SoundDevices {
    const val DEFAULT = "Predefinita di Windows"

    fun inputs(): List<String> = names(TargetDataLine::class.java)

    fun outputs(): List<String> = names(SourceDataLine::class.java)

    fun openInput(name: String?, bufferBytes: Int): Opened<TargetDataLine> =
        open(name, TargetDataLine::class.java, bufferBytes) { it.open(PCM_FORMAT, bufferBytes) }

    fun openOutput(name: String?, bufferBytes: Int): Opened<SourceDataLine> =
        open(name, SourceDataLine::class.java, bufferBytes) { it.open(PCM_FORMAT, bufferBytes) }

    private fun names(type: Class<out DataLine>): List<String> {
        val info = DataLine.Info(type, PCM_FORMAT)
        return AudioSystem.getMixerInfo().filter { AudioSystem.getMixer(it).isLineSupported(info) }.map { it.name }
    }

    private fun <T : DataLine> open(name: String?, type: Class<T>, bufferBytes: Int, openLine: (T) -> Unit): Opened<T> {
        val info = DataLine.Info(type, PCM_FORMAT, bufferBytes)
        val mixer = AudioSystem.getMixerInfo().firstOrNull { it.name == name }
        if (mixer != null) {
            runCatching {
                val line = type.cast(AudioSystem.getMixer(mixer).getLine(info))
                openLine(line)
                return Opened(line, mixer.name, fellBack = false)
            }
        }
        val line = type.cast(AudioSystem.getLine(info))
        openLine(line)
        return Opened(line, DEFAULT, fellBack = name != null && name != DEFAULT)
    }
}
